#!/bin/bash
# OpenPLC entrypoint script that automatically loads and compiles ST programs

set -e

WORKDIR="/workdir"
OPENPLC_DIR="/root/OpenPLC_v3/webserver"
ST_FILES_DIR="$OPENPLC_DIR/st_files"
DB_FILE="$OPENPLC_DIR/openplc.db"

echo "=== OpenPLC Auto-Configuration ==="

# Find ST file in workdir
ST_FILE=$(find "$WORKDIR" -maxdepth 1 -name "*.st" -type f | head -1)

if [ -z "$ST_FILE" ]; then
    echo "No ST file found in $WORKDIR, starting with blank program"
else
    ST_FILENAME=$(basename "$ST_FILE")
    echo "Found ST file: $ST_FILENAME"

    # Copy to OpenPLC st_files directory
    cp "$ST_FILE" "$ST_FILES_DIR/$ST_FILENAME"
    echo "Copied to $ST_FILES_DIR/$ST_FILENAME"

    # Compile the program
    echo "Compiling program..."
    cd "$OPENPLC_DIR"
    if ./scripts/compile_program.sh "$ST_FILENAME" 2>&1 | tail -5; then
        echo "Compilation successful!"

        # Update active_program file
        echo "$ST_FILENAME" > "$OPENPLC_DIR/active_program"

        # Update the database to register this program
        # First, check if database exists and has the Programs table
        if [ -f "$DB_FILE" ]; then
            # Get program name without extension
            PROG_NAME="${ST_FILENAME%.*}"

            # Insert or update the program in the database
            sqlite3 "$DB_FILE" "INSERT OR REPLACE INTO Programs (Name, Description, File, Date_upload) VALUES ('$PROG_NAME', 'Auto-loaded program', '$ST_FILENAME', datetime('now'));" 2>/dev/null || true

            # Set this as the current program in Settings
            sqlite3 "$DB_FILE" "UPDATE Settings SET Current_program = '$ST_FILENAME' WHERE Key = 'current_program';" 2>/dev/null || true
            sqlite3 "$DB_FILE" "INSERT OR IGNORE INTO Settings (Key, Value) VALUES ('current_program', '$ST_FILENAME');" 2>/dev/null || true

            # Enable auto-start
            sqlite3 "$DB_FILE" "UPDATE Settings SET Value = '1' WHERE Key = 'Start_run_mode';" 2>/dev/null || true
            sqlite3 "$DB_FILE" "INSERT OR IGNORE INTO Settings (Key, Value) VALUES ('Start_run_mode', '1');" 2>/dev/null || true

            echo "Database updated, auto-start enabled"
        else
            echo "Warning: Database not found, program may not auto-start"
        fi
    else
        echo "Compilation failed, starting with blank program"
    fi
fi

echo "=== Starting OpenPLC ==="

# Start OpenPLC in background
/root/OpenPLC_v3/start_openplc.sh &
OPENPLC_PID=$!

# Function to start PLC via API
start_plc_runtime() {
    local max_attempts=30
    local attempt=0

    echo "Waiting for OpenPLC web server..."
    while [ $attempt -lt $max_attempts ]; do
        if curl -s http://127.0.0.1:8080/ >/dev/null 2>&1; then
            echo "OpenPLC web server is ready"
            break
        fi
        attempt=$((attempt + 1))
        sleep 2
    done

    if [ $attempt -eq $max_attempts ]; then
        echo "Warning: OpenPLC web server not ready after ${max_attempts} attempts"
        return 1
    fi

    # Login to get session
    echo "Logging in to OpenPLC..."
    COOKIE_FILE=$(mktemp)
    curl -s -c "$COOKIE_FILE" -d "username=openplc&password=openplc" http://127.0.0.1:8080/login >/dev/null

    # Start the PLC runtime
    echo "Starting PLC runtime..."
    curl -s -b "$COOKIE_FILE" http://127.0.0.1:8080/start_plc >/dev/null

    rm -f "$COOKIE_FILE"

    # Verify Modbus is running
    sleep 3
    if netstat -tlnp 2>/dev/null | grep -q ":502 "; then
        echo "PLC runtime started - Modbus server active on port 502"
        return 0
    else
        echo "Warning: PLC started but Modbus port 502 not listening"
        return 1
    fi
}

# Start PLC runtime in background
start_plc_runtime &

# Wait for OpenPLC process
wait $OPENPLC_PID
