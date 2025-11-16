#!/bin/bash
#
# Quick Start Guide for Load Testing Framework
# Sets up everything needed to run load tests
#
# Usage: ./quick_start.sh
#

set -e

echo "=========================================="
echo "Milvus Load Testing - Quick Start"
echo "=========================================="
echo ""

# Check if Python is installed
if ! command -v python3 &> /dev/null; then
    echo "ERROR: Python 3 is required but not installed"
    exit 1
fi

echo "Step 1: Installing dependencies..."
pip install -r requirements.txt
echo "✓ Dependencies installed"
echo ""

# Check if Milvus is running
echo "Step 2: Checking Milvus connection..."
MILVUS_URI=${MILVUS_URI:-http://localhost:19530}

if python3 -c "
from pymilvus import MilvusClient
try:
    client = MilvusClient(uri='$MILVUS_URI')
    print('✓ Connected to Milvus at $MILVUS_URI')
except Exception as e:
    print('✗ Failed to connect to Milvus at $MILVUS_URI')
    print('  Error:', e)
    print()
    print('Please ensure Milvus is running:')
    print('  docker run -d --name milvus -p 19530:19530 milvusdb/milvus:latest')
    exit(1)
"; then
    echo ""
else
    exit 1
fi

# Check if test collection exists
echo "Step 3: Checking test collection..."
COLLECTION_NAME=${LOAD_TEST_COLLECTION:-load_test_collection}

if python3 -c "
from pymilvus import MilvusClient
client = MilvusClient(uri='$MILVUS_URI')
collections = client.list_collections()
if '$COLLECTION_NAME' in collections:
    print('✓ Collection \"$COLLECTION_NAME\" exists')
    exit(0)
else:
    print('✗ Collection \"$COLLECTION_NAME\" does not exist')
    exit(1)
" 2>/dev/null; then
    echo ""
else
    echo ""
    echo "Creating test collection (this may take a few minutes)..."
    python3 setup_test_collection.py \
        --uri "$MILVUS_URI" \
        --collection-name "$COLLECTION_NAME" \
        --num-vectors 10000 \
        --dimension 768
    echo ""
fi

echo "=========================================="
echo "Setup Complete! 🎉"
echo "=========================================="
echo ""
echo "Quick Start Options:"
echo ""
echo "1. Run with Web UI (interactive):"
echo "   locust -f realistic_workload.py"
echo "   Then open http://localhost:8089"
echo ""
echo "2. Run baseline test (headless):"
echo "   ./run_baseline_test.sh"
echo ""
echo "3. Run normal load test (headless):"
echo "   ./run_normal_load_test.sh"
echo ""
echo "4. Run stress test (headless):"
echo "   ./run_stress_test.sh"
echo ""
echo "5. Custom test:"
echo "   locust -f realistic_workload.py \\"
echo "     --headless --users 100 --spawn-rate 10 --run-time 30m"
echo ""
echo "=========================================="
echo "For more information, see README.md"
echo "=========================================="
