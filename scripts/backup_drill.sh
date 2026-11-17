#!/bin/sh
# The backup drill: proves that what scripts/backup.sh writes can be
# restored. It makes a scratch database and two scratch volumes, backs
# them up with the real recipe, restores the bundle into a second scratch
# database and second directories the way `make restore` does, and compares
# the two. It then does the same with an encrypted bundle, and checks what
# must not work: a wrong passphrase, a dump that fails, a bundle past its
# retention.
#
# It needs a Postgres it may create databases on, and the pg_dump, psql and
# openssl commands. Nothing outside its scratch databases and a temporary
# directory is touched.
#
#   make backup-drill                       (reads OPENV_TEST_DATABASE_URL)
#   DB_HOST=127.0.0.1 DB_PORT=5432 DB_USER=postgres sh scripts/backup_drill.sh
#
# Exit status 0 means every check passed.

set -eu

here="$(cd "$(dirname "$0")" && pwd)"
DB_HOST="${DB_HOST:-127.0.0.1}"
DB_PORT="${DB_PORT:-5432}"
DB_USER="${DB_USER:-postgres}"
export DB_HOST DB_PORT DB_USER

work="$(mktemp -d)"
src="drill_src_$$"
dst="drill_dst_$$"
checks=0

psql_q() { psql -h "$DB_HOST" -p "$DB_PORT" -U "$DB_USER" -v ON_ERROR_STOP=1 -q -At "$@"; }
finish() {
	psql_q -d postgres -c "DROP DATABASE IF EXISTS $src" -c "DROP DATABASE IF EXISTS $dst" >/dev/null 2>&1 || true
	rm -rf "$work"
}
trap finish EXIT INT TERM

ok() { checks=$((checks + 1)); printf 'ok   %s\n' "$1"; }
fail() { printf 'FAIL %s\n' "$1" >&2; exit 1; }

# fingerprint <db>: every row of the scratch table, in order.
fingerprint() { psql_q -d "$1" -c "SELECT md5(string_agg(id::text || ':' || body, '|' ORDER BY id)) FROM drill_rows"; }
# tree <dir>: every file's path and checksum.
tree() { (cd "$1" && find . -type f | sort | while read -r f; do printf '%s %s\n' "$f" "$(md5sum <"$f")"; done); }

# backup <extra env...>: one run of the real recipe into $work/backups.
backup() { env DB_NAME="$src" DATA_DIR="$work/data" UPLOADS_DIR="$work/uploads" BACKUP_DIR="$work/backups" "$@" sh "$here/backup.sh" --once >"$work/log" 2>&1; }
newest() { ls -t "$work/backups"/openv-backup-* | head -1; }

# restore <bundle.tar.gz>: what `make restore` does, into the second
# database and directories.
restore() {
	rm -rf "$work/restore" "$work/data2" "$work/uploads2"
	mkdir -p "$work/restore" "$work/data2" "$work/uploads2"
	tar xzf "$1" -C "$work/restore"
	tar xzf "$work/restore/openv-data.tar.gz" -C "$work/data2"
	tar xzf "$work/restore/uploads-data.tar.gz" -C "$work/uploads2"
	psql_q -d postgres -c "DROP DATABASE IF EXISTS $dst" -c "CREATE DATABASE $dst" >/dev/null
	psql_q -d "$dst" -f "$work/restore/openv-db.sql" >/dev/null
}
same() {
	[ "$(fingerprint "$src")" = "$(fingerprint "$dst")" ] || fail "$1: the database differs after restore"
	[ "$(tree "$work/data")" = "$(tree "$work/data2")" ] || fail "$1: the data volume differs after restore"
	[ "$(tree "$work/uploads")" = "$(tree "$work/uploads2")" ] || fail "$1: the uploads volume differs after restore"
	ok "$1"
}

# What is backed up: 500 rows, and files in both volumes (one with a space
# in its name, one binary).
psql_q -d postgres -c "CREATE DATABASE $src" >/dev/null
psql_q -d "$src" -c "CREATE TABLE drill_rows (id serial PRIMARY KEY, body text NOT NULL)" \
	-c "INSERT INTO drill_rows (body) SELECT md5(g::text) FROM generate_series(1, 500) g" >/dev/null
mkdir -p "$work/data/keys" "$work/uploads/figures" "$work/backups"
printf 'secrets key material\n' >"$work/data/keys/secrets.key"
printf 'a drawing\n' >"$work/uploads/figures/REQ-1 FIG-1.png"
head -c 4096 /dev/urandom >"$work/uploads/figures/scan.bin"

# 1. A plain bundle restores to the same database and the same files.
backup
plain="$(newest)"
case "$plain" in *.tar.gz) ;; *) fail "a plain bundle is a .tar.gz: $plain" ;; esac
restore "$plain"
same "a plain bundle restores whole"
[ -z "$(find "$work/backups" -name '.*' -mindepth 1)" ] || fail "a finished run leaves no staging or partial file"
ok "a finished run leaves nothing half-written behind"

# 2. An encrypted bundle is not readable as it lies, restores with its
# passphrase, and not with another.
sleep 1
backup BACKUP_PASSPHRASE='correct horse battery staple'
enc="$(newest)"
case "$enc" in *.tar.gz.enc) ;; *) fail "an encrypted bundle is a .tar.gz.enc: $enc" ;; esac
tar tzf "$enc" >/dev/null 2>&1 && fail "an encrypted bundle still reads as a tar"
grep -q 'drill_rows' "$enc" && fail "an encrypted bundle holds the dump in the clear"
ok "an encrypted bundle cannot be read as it lies"
if BACKUP_PASSPHRASE='not the passphrase' sh "$here/backup.sh" --decrypt "$enc" "$work/wrong.tar.gz" >/dev/null 2>&1 && tar tzf "$work/wrong.tar.gz" >/dev/null 2>&1; then
	fail "a wrong passphrase decrypted the bundle"
fi
ok "a wrong passphrase does not open it"
printf 'correct horse battery staple' >"$work/pass"
BACKUP_PASSPHRASE_FILE="$work/pass" sh "$here/backup.sh" --decrypt "$enc" "$work/opened.tar.gz" >/dev/null 2>&1
psql_q -d "$src" -c "UPDATE drill_rows SET body = body" >/dev/null
restore "$work/opened.tar.gz"
same "an encrypted bundle restores whole, its passphrase read from a file"

# 3. A bundle goes off the host: a copy into a second directory, and the hook.
sleep 1
backup BACKUP_COPY_DIR="$work/offsite" BACKUP_POST_HOOK="cp \"\$BACKUP_FILE\" \"$work/hooked\""
[ -f "$work/offsite/$(basename "$(newest)")" ] || fail "the bundle was not copied to BACKUP_COPY_DIR"
cmp -s "$(newest)" "$work/hooked" || fail "the hook was not given the bundle"
ok "a bundle is copied off the host and handed to the hook"

# 4. A run that cannot dump the database writes no bundle, once and in the
# loop the sidecar runs.
sleep 1
before="$(ls "$work/backups" | wc -l)"
sums="$(md5sum "$work/backups"/* | md5sum)"
if env DB_NAME="no_such_db_$$" DATA_DIR="$work/data" UPLOADS_DIR="$work/uploads" BACKUP_DIR="$work/backups" sh "$here/backup.sh" --once >/dev/null 2>&1; then
	fail "a backup of a database that is not there reported success"
fi
env DB_NAME="no_such_db_$$" DATA_DIR="$work/data" UPLOADS_DIR="$work/uploads" BACKUP_DIR="$work/backups" BACKUP_INTERVAL_SECONDS=60 sh "$here/backup.sh" --loop >/dev/null 2>&1 &
loop=$!
sleep 3
kill "$loop" 2>/dev/null || true
wait "$loop" 2>/dev/null || true
[ "$(ls "$work/backups" | wc -l)" = "$before" ] || fail "a run whose dump failed still wrote a bundle"
[ "$(md5sum "$work/backups"/* | md5sum)" = "$sums" ] || fail "a run whose dump failed overwrote a bundle"
ok "a run whose dump fails writes no bundle"

# 5. A bundle past its retention is pruned, plain or encrypted; a newer one is kept.
touch -d '10 days ago' "$work/backups/openv-backup-20000101-000000.tar.gz" "$work/backups/openv-backup-20000101-000001.tar.gz.enc"
sleep 1
backup BACKUP_RETENTION_DAYS=7
[ ! -e "$work/backups/openv-backup-20000101-000000.tar.gz" ] && [ ! -e "$work/backups/openv-backup-20000101-000001.tar.gz.enc" ] || fail "an old bundle was not pruned"
[ -f "$plain" ] || fail "a bundle inside its retention was pruned"
ok "old bundles are pruned and recent ones kept"

printf 'backup drill: %s checks passed\n' "$checks"
