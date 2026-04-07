#!/usr/bin/env bash
# Run all three scenarios in sequence (auto mode — no pauses)
# Usage: ./run-all.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "=========================================="
echo "  Running all scenarios in AUTO_RUN mode"
echo "=========================================="
echo ""

export AUTO_RUN=1

"${SCRIPT_DIR}/scenario1-enterprise-to-breaker.sh"
echo ""
echo "---"
echo ""
"${SCRIPT_DIR}/scenario2-vendor-access-abuse.sh"
echo ""
echo "---"
echo ""
"${SCRIPT_DIR}/scenario3-ot-east-west-pivot.sh"

echo ""
echo "=========================================="
echo "  All scenarios complete"
echo "=========================================="
