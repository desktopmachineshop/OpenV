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

log() { printf '%s backup: %s\n' "$(date +%Y-%m-%dT%H:%M:%S)" "$*"; }

# prune deletes bundles strictly older than BACKUP_RETENTION_DAYS. A value of
# 0 (or empty) disables pruning so nothing is ever removed automatically.
prune() {
	if [ -z "$BACKUP_RETENTION_DAYS" ] || [ "$BACKUP_RETENTION_DAYS" -le 0 ]; then
		log "retention disabled (BACKUP_RETENTION_DAYS=$BACKUP_RETENTION_DAYS); keeping all bundles"
		return 0
	fi
	log "pruning bundles older than $BACKUP_RETENTION_DAYS day(s)"
	# -mtime +N matches files last modified more than N*24h ago.
	find "$BACKUP_DIR" -maxdepth 1 -type f \
		-name "${BACKUP_PREFIX}-*.tar.gz" \
		-mtime +"$BACKUP_RETENTION_DAYS" -print -exec rm -f {} +
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

	cleanup() { rm -rf "$stage" "$tmp_bundle"; }
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
	mv "$tmp_bundle" "$final_bundle"

	rm -rf "$stage"
	trap - EXIT INT TERM

	log "wrote $final_bundle"
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
			if ! backup_once; then
				log "backup run failed; will retry after interval"
			fi
			sleep "$BACKUP_INTERVAL_SECONDS"
		done
		;;
	*)
		echo "usage: backup.sh [--once|--loop]" >&2
		exit 2
		;;
	esac
}

main "$@"
