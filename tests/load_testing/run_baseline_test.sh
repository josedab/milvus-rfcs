#!/bin/bash
#
# Baseline Performance Test
# Establishes baseline metrics with light load
#
# Usage: ./run_baseline_test.sh [collection_name]
#

set -e

COLLECTION_NAME=${1:-load_test_collection}
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
OUTPUT_DIR="results/baseline_${TIMESTAMP}"

echo "=========================================="
echo "Baseline Performance Test"
echo "=========================================="
echo "Collection: $COLLECTION_NAME"
echo "Output: $OUTPUT_DIR"
echo "=========================================="

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Set configuration
export LOAD_TEST_COLLECTION="$COLLECTION_NAME"

# Run test
locust -f realistic_workload.py \
    --headless \
    --users 10 \
    --spawn-rate 2 \
    --run-time 5m \
    --csv="$OUTPUT_DIR/baseline" \
    --html="$OUTPUT_DIR/baseline_report.html" \
    --logfile="$OUTPUT_DIR/baseline.log"

echo ""
echo "=========================================="
echo "Test completed!"
echo "Results saved to: $OUTPUT_DIR"
echo "=========================================="
echo "View results:"
echo "  HTML Report: $OUTPUT_DIR/baseline_report.html"
echo "  CSV Stats:   $OUTPUT_DIR/baseline_stats.csv"
echo "  Logs:        $OUTPUT_DIR/baseline.log"
echo "=========================================="
