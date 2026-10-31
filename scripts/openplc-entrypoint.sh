#!/bin/bash
# Prefer a workdir ST override; otherwise install the verified baked seed.
set -euo pipefail

/usr/local/bin/set-gateway.sh

WORKDIR="/workdir"
OPENPLC_DIR="/root/OpenPLC_v3/webserver"
ST_FILES_DIR="$OPENPLC_DIR/st_files"
DB_FILE="$OPENPLC_DIR/openplc.db"
SEED_DIR="/opt/openplc-us"
SEED_PROGRAM="substation-automation.st"
USING_BAKED_SEED=false

echo "=== OpenPLC Auto-Configuration ==="

if ! (cd "$SEED_DIR" && sha256sum --check seed.sha256); then
    echo "ERROR: baked US OpenPLC seed hash verification failed" >&2
    exit 1
fi
SEED_HASH=$(sha256sum "$SEED_DIR/seed.sha256" | cut -d ' ' -f 1)

ST_FILE=""
if [ -d "$WORKDIR" ]; then
    ST_FILE=$(find "$WORKDIR" -maxdepth 1 -name "*.st" -type f -print -quit)
fi

if [ -n "$ST_FILE" ]; then
    ST_FILENAME=$(basename "$ST_FILE")
    echo "Found workdir ST override: $ST_FILENAME"
else
    ST_FILENAME="$SEED_PROGRAM"
    ST_FILE="$SEED_DIR/$SEED_PROGRAM"
    USING_BAKED_SEED=true
    echo "No ST override found in $WORKDIR; using baked US OpenPLC seed"
fi

cp "$ST_FILE" "$ST_FILES_DIR/$ST_FILENAME"
echo "Compiling $ST_FILENAME..."
cd "$OPENPLC_DIR"
rm -f core/openplc
if ./scripts/compile_program.sh "$ST_FILENAME" 2>&1 | tail -5; then
    if [ ! -x core/openplc ]; then
        echo "ERROR: compiler completed without producing core/openplc for $ST_FILENAME" >&2
        exit 1
    fi
    echo "Compilation successful!"
else
    echo "ERROR: Compilation failed for $ST_FILENAME; refusing to start OpenPLC" >&2
    exit 1
fi

printf '%s\n' "$ST_FILENAME" > "$OPENPLC_DIR/active_program"
if ! test "$(cat "$OPENPLC_DIR/active_program")" = "$ST_FILENAME"; then
    echo "ERROR: failed to select $ST_FILENAME as the active program" >&2
    exit 1
fi

if [ "$USING_BAKED_SEED" = true ]; then
    if ! python3 "$SEED_DIR/install.py"; then
        echo "ERROR: failed to install and verify the baked US OpenPLC seed database" >&2
        exit 1
    fi
    printf '%s\n' "$SEED_HASH" > /var/lib/openplc-us-seed.sha256
    echo "US OpenPLC seed sha256: $SEED_HASH"
else
    PROG_NAME="${ST_FILENAME%.*}"
    if [ ! -f "$DB_FILE" ]; then
        echo "ERROR: OpenPLC database not found at $DB_FILE; cannot register $ST_FILENAME" >&2
        exit 1
    fi
    if ! sqlite3 "$DB_FILE" "INSERT OR REPLACE INTO Programs (Name, Description, File, Date_upload) VALUES ('$PROG_NAME', 'Auto-loaded program', '$ST_FILENAME', datetime('now'));" 2>/dev/null; then
        echo "ERROR: Failed to register $ST_FILENAME in the OpenPLC database" >&2
        exit 1
    fi
    if ! sqlite3 "$DB_FILE" "UPDATE Settings SET Current_program = '$ST_FILENAME' WHERE Key = 'current_program';" 2>/dev/null; then
        echo "ERROR: Failed to set the current program in the OpenPLC database" >&2
        exit 1
    fi
    if ! sqlite3 "$DB_FILE" "INSERT OR IGNORE INTO Settings (Key, Value) VALUES ('current_program', '$ST_FILENAME');" 2>/dev/null; then
        echo "ERROR: Failed to register the current program setting in the OpenPLC database" >&2
        exit 1
    fi
    if ! sqlite3 "$DB_FILE" "UPDATE Settings SET Value = '1' WHERE Key = 'Start_run_mode';" 2>/dev/null; then
        echo "ERROR: Failed to enable OpenPLC auto-start in the database" >&2
        exit 1
    fi
    if ! sqlite3 "$DB_FILE" "INSERT OR IGNORE INTO Settings (Key, Value) VALUES ('Start_run_mode', '1');" 2>/dev/null; then
        echo "ERROR: Failed to register OpenPLC auto-start in the database" >&2
        exit 1
    fi
    echo "Database updated, auto-start enabled"
fi

echo "=== Starting OpenPLC ==="
/root/OpenPLC_v3/start_openplc.sh &
OPENPLC_PID=$!
trap 'kill "$OPENPLC_PID" 2>/dev/null || true' EXIT
trap 'exit 143' TERM
trap 'exit 130' INT

echo "Waiting for OpenPLC web server..."
for attempt in {1..60}; do
    if curl -fsS --max-time 1 http://127.0.0.1:8080/ >/dev/null 2>&1; then
        break
    fi
    if [ "$attempt" -eq 60 ]; then
        echo "ERROR: OpenPLC web server did not become ready after 60 attempts" >&2
        exit 1
    fi
    sleep 1
done

COOKIE_FILE=$(mktemp)
if ! curl -fsS --max-time 5 -c "$COOKIE_FILE" \
    -d "username=openplc&password=openplc" http://127.0.0.1:8080/login >/dev/null; then
    echo "ERROR: failed to log in to the OpenPLC web server" >&2
    rm -f "$COOKIE_FILE"
    exit 1
fi
if ! curl -fsS --max-time 5 -b "$COOKIE_FILE" \
    http://127.0.0.1:8080/start_plc >/dev/null; then
    echo "ERROR: failed to start the OpenPLC runtime" >&2
    rm -f "$COOKIE_FILE"
    exit 1
fi
rm -f "$COOKIE_FILE"

if [ "$USING_BAKED_SEED" = true ]; then
    if ! python3 "$SEED_DIR/check_runtime.py"; then
        echo "ERROR: seeded OpenPLC Modbus status/heartbeat check failed" >&2
        exit 1
    fi
else
    sleep 3
    if ! netstat -tlnp 2>/dev/null | grep -q ":502 "; then
        echo "ERROR: OpenPLC runtime did not listen on Modbus port 502" >&2
        exit 1
    fi
fi

if ! wait "$OPENPLC_PID"; then
    echo "ERROR: OpenPLC process exited unsuccessfully" >&2
    exit 1
fi
