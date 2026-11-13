#!/bin/sh
sra_check() {
    if [ -f /var/lib/sra/cleanup-complete ] && [ ! -f /var/lib/sra/seed-complete ]; then
        echo "sra: interrupted initialization; recreate container" >&2
        exit 1
    fi
}
sra_begin() { touch /var/lib/sra/cleanup-complete; }
sra_done() { touch /var/lib/sra/seed-complete; }
