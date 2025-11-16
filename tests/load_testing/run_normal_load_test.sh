#!/bin/bash
#
# Normal Production Load Test
# Simulates typical production traffic patterns
#
# Usage: ./run_normal_load_test.sh [collection_name]
#

set -e

COLLECTION_NAME=${1:-load_test_collection}
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
OUTPUT_DIR="results/normal_load_${TIMESTAMP}"

echo "=========================================="
echo "Normal Production Load Test"
echo "=========================================="
echo "Collection: $COLLECTION_NAME"
echo "Users: 100"
echo "Duration: 30 minutes"
echo "Output: $OUTPUT_DIR"
echo "=========================================="

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Set configuration
export LOAD_TEST_COLLECTION="$COLLECTION_NAME"

# Run test
locust -f realistic_workload.py \
    --headless \
    --users 100 \
    --spawn-rate 10 \
    --run-time 30m \
    --csv="$OUTPUT_DIR/normal_load" \
    --html="$OUTPUT_DIR/normal_load_report.html" \
    --logfile="$OUTPUT_DIR/normal_load.log"

echo ""
echo "=========================================="
echo "Test completed!"
echo "Results saved to: $OUTPUT_DIR"
echo "=========================================="
echo "View results:"
echo "  HTML Report: $OUTPUT_DIR/normal_load_report.html"
echo "  CSV Stats:   $OUTPUT_DIR/normal_load_stats.csv"
echo "  Logs:        $OUTPUT_DIR/normal_load.log"
echo "=========================================="
