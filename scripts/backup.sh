#!/bin/sh
# OpenV backup recipe — the single source of truth shared by the manual
# `make backup` target and the opt-in backup sidecar
# (docker-compose.backup.yml). See docs/operations.md.
#
# It is designed to run *inside a container* that has:
#   - network access to Postgres (DB_HOST/DB_PORT), so pg_dump needs no host
#     client tools and always matches the server version;
#   - the openv-data and uploads volumes mounted (DATA_DIR, UPLOADS_DIR),
#     read-only is fine;
#   - a writable backup target mounted (BACKUP_DIR).
#
# It produces one timestamped bundle identical in layout to the manual
# `make backup` output, so `make restore` accepts either:
#
#   <BACKUP_DIR>/openv-backup-<YYYYMMDD-HHMMSS>.tar.gz
#     |- openv-db.sql          pg_dump --clean --if-exists of DB_NAME
#     |- openv-data.tar.gz     the openv-data volume  (DATA_DIR)
#     `- uploads-data.tar.gz   the uploads volume     (UPLOADS_DIR)
#
# Modes:
#   backup.sh            run one backup + prune, then exit (default)
#   backup.sh --once     same as default
#   backup.sh --loop     run forever: backup + prune, sleep, repeat
#   backup.sh --decrypt <in> <out>   decrypt an encrypted bundle
#
# Configuration (all via environment, with safe defaults):
#   DB_HOST=postgres  DB_PORT=5432  DB_USER=postgres  DB_NAME=openv
#   PGPASSWORD                       Postgres password (read by pg_dump)
#   DATA_DIR=/data                   openv-data volume mount
#   UPLOADS_DIR=/uploads             uploads volume mount
#   BACKUP_DIR=/backups              where bundles are written
#   BACKUP_PREFIX=openv-backup       bundle filename prefix
#   BACKUP_RETENTION_DAYS=7          prune bundles strictly older than this
#                                    many days (0 disables pruning)
#   BACKUP_INTERVAL_SECONDS=86400    --loop sleep between runs (default daily)
#   BACKUP_PASSPHRASE                encrypt each bundle with this passphrase
#   BACKUP_PASSPHRASE_FILE           ... or with the one in this file (a
#                                    mounted secret). Either makes the bundle
#                                    <prefix>-<stamp>.tar.gz.enc: AES-256-CBC,
#                                    key from PBKDF2. Needs the openssl
#                                    command, which is installed with apk
#                                    where the image has apk and lacks it.
#   BACKUP_COPY_DIR                  a second directory each finished bundle
#                                    is copied to: mount storage that is not
#                                    this host's disk there. Pruned as
#                                    BACKUP_DIR is.
#   BACKUP_POST_HOOK                 a shell command run after each bundle,
#                                    with BACKUP_FILE set to its path: push
#                                    it wherever the copy belongs. A hook
#                                    that fails fails the run.
#
# Restoring an encrypted bundle: `make restore` decrypts it with the same
# passphrase, or by hand:
#   backup.sh --decrypt <bundle.tar.gz.enc> <bundle.tar.gz>

set -eu

DB_HOST="${DB_HOST:-postgres}"
DB_PORT="${DB_PORT:-5432}"
DB_USER="${DB_USER:-postgres}"
DB_NAME="${DB_NAME:-openv}"
DATA_DIR="${DATA_DIR:-/data}"
UPLOADS_DIR="${UPLOADS_DIR:-/uploads}"
BACKUP_DIR="${BACKUP_DIR:-/backups}"
BACKUP_PREFIX="${BACKUP_PREFIX:-openv-backup}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-7}"
BACKUP_INTERVAL_SECONDS="${BACKUP_INTERVAL_SECONDS:-86400}"

BACKUP_PASSPHRASE="${BACKUP_PASSPHRASE:-}"
BACKUP_PASSPHRASE_FILE="${BACKUP_PASSPHRASE_FILE:-}"
BACKUP_COPY_DIR="${BACKUP_COPY_DIR:-}"
BACKUP_POST_HOOK="${BACKUP_POST_HOOK:-}"
# PBKDF2 rounds for the key. Written here once: a bundle is decrypted with
# the rounds it was encrypted with, so this number never changes.
BACKUP_KDF_ITERATIONS=200000

log() { printf '%s backup: %s\n' "$(date +%Y-%m-%dT%H:%M:%S)" "$*"; }

# encrypting reports whether bundles are encrypted.
encrypting() { [ -n "$BACKUP_PASSPHRASE" ] || [ -n "$BACKUP_PASSPHRASE_FILE" ]; }

# passphrase_arg is the -pass argument openssl reads the passphrase from. It
# never puts the passphrase on a command line, where ps would show it.
passphrase_arg() {
	if [ -n "$BACKUP_PASSPHRASE_FILE" ]; then
		[ -r "$BACKUP_PASSPHRASE_FILE" ] || { log "cannot read BACKUP_PASSPHRASE_FILE ($BACKUP_PASSPHRASE_FILE)"; return 1; }
		printf 'file:%s' "$BACKUP_PASSPHRASE_FILE"
	else
		export BACKUP_PASSPHRASE
		printf 'env:BACKUP_PASSPHRASE'
	fi
}

# need_openssl makes sure the openssl command is there, installing it where
# the image is Alpine and lacks it (the stock postgres image does).
need_openssl() {
	command -v openssl >/dev/null 2>&1 && return 0
	if command -v apk >/dev/null 2>&1; then
		log "installing openssl to encrypt bundles"
		apk add --no-cache openssl >/dev/null 2>&1 || true
	fi
	command -v openssl >/dev/null 2>&1 || { log "a passphrase is set but the openssl command is missing: no bundle written"; return 1; }
}

# encrypt_file <in> <out> and decrypt_file <in> <out>.
encrypt_file() {
	need_openssl || return 1
	pass="$(passphrase_arg)" || return 1
	openssl enc -aes-256-cbc -pbkdf2 -iter "$BACKUP_KDF_ITERATIONS" -salt -pass "$pass" -in "$1" -out "$2"
}
decrypt_file() {
	encrypting || { log "set BACKUP_PASSPHRASE or BACKUP_PASSPHRASE_FILE to decrypt"; return 1; }
	need_openssl || return 1
	pass="$(passphrase_arg)" || return 1
	# Under a temporary name until it is whole: a wrong passphrase leaves
	# nothing that looks like a bundle.
	if openssl enc -d -aes-256-cbc -pbkdf2 -iter "$BACKUP_KDF_ITERATIONS" -pass "$pass" -in "$1" -out "$2.partial" 2>/dev/null &&
		tar tzf "$2.partial" >/dev/null 2>&1; then
		mv "$2.partial" "$2"
	else
		unlink "$2.partial" 2>/dev/null || true
		log "could not decrypt $1: is the passphrase right?"
		return 1
	fi
}

# prune deletes bundles strictly older than BACKUP_RETENTION_DAYS. A value of
# 0 (or empty) disables pruning so nothing is ever removed automatically.
prune() {
	if [ -z "$BACKUP_RETENTION_DAYS" ] || [ "$BACKUP_RETENTION_DAYS" -le 0 ]; then
		log "retention disabled (BACKUP_RETENTION_DAYS=$BACKUP_RETENTION_DAYS); keeping all bundles"
		return 0
	fi
	log "pruning bundles older than $BACKUP_RETENTION_DAYS day(s)"
	# -mtime +N matches files last modified more than N*24h ago.
	for dir in "$BACKUP_DIR" "$BACKUP_COPY_DIR"; do
		[ -n "$dir" ] && [ -d "$dir" ] || continue
		find "$dir" -maxdepth 1 -type f \
			\( -name "${BACKUP_PREFIX}-*.tar.gz" -o -name "${BACKUP_PREFIX}-*.tar.gz.enc" \) \
			-mtime +"$BACKUP_RETENTION_DAYS" -print -exec rm -f {} +
	done
}

# deliver takes a finished bundle off the host: a copy into BACKUP_COPY_DIR,
# under a temporary name until it is whole, then the hook.
deliver() {
	bundle="$1"
	if [ -n "$BACKUP_COPY_DIR" ]; then
		mkdir -p "$BACKUP_COPY_DIR"
		name="$(basename "$bundle")"
		cp "$bundle" "$BACKUP_COPY_DIR/.$name.partial"
		mv "$BACKUP_COPY_DIR/.$name.partial" "$BACKUP_COPY_DIR/$name"
		log "copied to $BACKUP_COPY_DIR/$name"
	fi
	if [ -n "$BACKUP_POST_HOOK" ]; then
		log "running BACKUP_POST_HOOK"
		BACKUP_FILE="$bundle" sh -c "$BACKUP_POST_HOOK"
	fi
}

backup_once() {
	mkdir -p "$BACKUP_DIR"

	stamp="$(date +%Y%m%d-%H%M%S)"
	# Stage inside the backup target (not container /tmp) so we use the
	# backup volume's space, mirroring the manual make recipe.
	stage="$BACKUP_DIR/.stage-$stamp"
	# Write to a hidden temp name first and rename only on success, so a
	# partial/interrupted run never leaves a bundle that looks complete.
	tmp_bundle="$BACKUP_DIR/.${BACKUP_PREFIX}-$stamp.tar.gz.partial"
	final_bundle="$BACKUP_DIR/${BACKUP_PREFIX}-$stamp.tar.gz"

	if encrypting; then
		# Fail before anything is dumped, not after.
		need_openssl
		passphrase_arg >/dev/null
		final_bundle="$final_bundle.enc"
	fi

	cleanup() { rm -rf "$stage" "$tmp_bundle" "$tmp_bundle.enc"; }
	trap cleanup EXIT INT TERM

	rm -rf "$stage"
	mkdir -p "$stage"

	log "dumping database '$DB_NAME' from $DB_HOST:$DB_PORT"
	pg_dump -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -d "$DB_NAME" \
		--clean --if-exists -f "$stage/openv-db.sql"

	log "archiving data volume ($DATA_DIR)"
	tar czf "$stage/openv-data.tar.gz" -C "$DATA_DIR" .

	log "archiving uploads volume ($UPLOADS_DIR)"
	tar czf "$stage/uploads-data.tar.gz" -C "$UPLOADS_DIR" .

	log "bundling"
	tar czf "$tmp_bundle" -C "$stage" .
	if encrypting; then
		log "encrypting"
		encrypt_file "$tmp_bundle" "$tmp_bundle.enc"
		rm -f "$tmp_bundle"
		mv "$tmp_bundle.enc" "$final_bundle"
	else
		mv "$tmp_bundle" "$final_bundle"
	fi

	rm -rf "$stage"
	trap - EXIT INT TERM

	log "wrote $final_bundle"
	deliver "$final_bundle"
	prune
}

main() {
	mode="${1:---once}"
	case "$mode" in
	--once | "")
		backup_once
		;;
	--loop)
		log "loop mode: interval ${BACKUP_INTERVAL_SECONDS}s, retention ${BACKUP_RETENTION_DAYS}d"
		while true; do
			# Never let one failed run kill the loop; log and retry next tick.
			# The run is a process of its own: called as a function in this
			# condition, the shell would ignore `set -e` inside it, and a run
			# whose pg_dump failed would go on to write a bundle with no
			# database in it.
			if ! sh "$0" --once; then
				log "backup run failed; will retry after interval"
			fi
			sleep "$BACKUP_INTERVAL_SECONDS"
		done
		;;
	--decrypt)
		[ $# -eq 3 ] || { echo "usage: backup.sh --decrypt <bundle.tar.gz.enc> <bundle.tar.gz>" >&2; exit 2; }
		decrypt_file "$2" "$3"
		;;
	*)
		echo "usage: backup.sh [--once|--loop|--decrypt <in> <out>]" >&2
		exit 2
		;;
	esac
}

main "$@"
