#!/bin/bash
#
# Stress Test
# Pushes system to limits to find breaking point
#
# Usage: ./run_stress_test.sh [collection_name]
#

set -e

COLLECTION_NAME=${1:-load_test_collection}
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
OUTPUT_DIR="results/stress_test_${TIMESTAMP}"

echo "=========================================="
echo "Stress Test - Find Breaking Point"
echo "=========================================="
echo "Collection: $COLLECTION_NAME"
echo "Users: 1000"
echo "Duration: 1 hour"
echo "Output: $OUTPUT_DIR"
echo "=========================================="
echo "WARNING: This will generate heavy load!"
echo "Press Ctrl+C within 5 seconds to cancel..."
sleep 5

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Set configuration
export LOAD_TEST_COLLECTION="$COLLECTION_NAME"

# Run test
locust -f realistic_workload.py \
    --headless \
    --users 1000 \
    --spawn-rate 50 \
    --run-time 1h \
    --csv="$OUTPUT_DIR/stress_test" \
    --html="$OUTPUT_DIR/stress_test_report.html" \
    --logfile="$OUTPUT_DIR/stress_test.log"

echo ""
echo "=========================================="
echo "Test completed!"
echo "Results saved to: $OUTPUT_DIR"
echo "=========================================="
echo "View results:"
echo "  HTML Report: $OUTPUT_DIR/stress_test_report.html"
echo "  CSV Stats:   $OUTPUT_DIR/stress_test_stats.csv"
echo "  Logs:        $OUTPUT_DIR/stress_test.log"
echo "=========================================="
