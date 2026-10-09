#!/bin/sh
# Regenerate the Apache-2.0 IEC 104 reference captures from the checked harness.
set -eu

HERE=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
SVC=$(dirname -- "$HERE")
TSHARK=${TSHARK:-tshark}
EDITCAP=${EDITCAP:-editcap}

command -v "$TSHARK" >/dev/null 2>&1 || {
    echo "tshark not found (set TSHARK)" >&2
    exit 2
}
command -v "$EDITCAP" >/dev/null 2>&1 || {
    echo "editcap not found (set EDITCAP)" >&2
    exit 2
}
command -v python3 >/dev/null 2>&1 || {
    echo "python3 not found" >&2
    exit 2
}
command -v docker >/dev/null 2>&1 || {
    echo "docker not found" >&2
    exit 2
}

tshark_version=$("$TSHARK" --version | awk 'NR == 1 { print $3 }')
editcap_version=$("$EDITCAP" --version | awk 'NR == 1 { print $3 }')
[ -n "$tshark_version" ] && [ "$tshark_version" = "$editcap_version" ] || {
    echo "tshark and editcap must be the same Wireshark version" >&2
    exit 2
}

TMP=$(mktemp -d "$HERE/.regenerate.XXXXXX")
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
RUN_OUT=$TMP/run
STAGE=$TMP/output
mkdir -p "$RUN_OUT" "$STAGE"

export TSHARK
bash "$SVC/check/run.sh" "$RUN_OUT"
INPUT=$RUN_OUT/iec104.pcap
[ -s "$INPUT" ] || {
    echo "check/run.sh did not produce $INPUT" >&2
    exit 1
}

# Select whole TCP conversations by their unique iec104cmd source IPs. The
# control-centre streams are deliberately included in both captures that need
# its startup/GI and session-recovery traffic.
write_stream_capture() {
    name=$1
    shift
    ip_filter=
    for ip do
        if [ -n "$ip_filter" ]; then
            ip_filter="$ip_filter || "
        fi
        ip_filter="${ip_filter}ip.addr == $ip"
    done

    streams=$("$TSHARK" -r "$INPUT" -Y "tcp.port == 2404 && ($ip_filter)" \
        -T fields -e tcp.stream | sort -n -u)
    [ -n "$streams" ] || {
        echo "no TCP/2404 stream found for $name" >&2
        return 1
    }
    stream_filter=$(printf '%s\n' "$streams" | awk '
        NF {
            if (n++) {
                printf " || "
            }
            printf "tcp.stream == %s", $1
        }
    ')
    "$TSHARK" -r "$INPUT" -Y "$stream_filter" -w "$TMP/$name.pcapng"
    "$EDITCAP" -F pcap "$TMP/$name.pcapng" "$STAGE/$name.pcap"
}

write_stream_capture startup-gi-monitor 10.204.104.20 10.204.104.35
write_stream_capture sbo-double-command 10.204.104.30 10.204.104.36 10.204.104.31 10.204.104.32 10.204.104.33
write_stream_capture session-management 10.204.104.20 10.204.104.34
write_stream_capture edge-addressing 10.204.104.35 10.204.104.37 10.204.104.38

python3 - "$STAGE" <<'PY'
import hashlib
import pathlib
import sys

directory = pathlib.Path(sys.argv[1])
names = (
    "startup-gi-monitor.pcap",
    "sbo-double-command.pcap",
    "session-management.pcap",
    "edge-addressing.pcap",
)
with (directory / "SHA256SUMS").open("w", encoding="ascii") as sums:
    for name in names:
        digest = hashlib.sha256((directory / name).read_bytes()).hexdigest()
        sums.write(f"{digest}  {name}\n")
PY

for artifact in \
    startup-gi-monitor.pcap \
    sbo-double-command.pcap \
    session-management.pcap \
    edge-addressing.pcap \
    SHA256SUMS
do
    mv "$STAGE/$artifact" "$HERE/$artifact"
done

echo "IEC 104 captures and SHA256SUMS regenerated in $HERE"
