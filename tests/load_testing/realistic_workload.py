#!/usr/bin/env python3
"""
Production-realistic load testing for Milvus

Simulates real query patterns, data distributions, and concurrency.
This framework uses Locust to generate realistic workloads that mirror
production traffic patterns, helping to:
- Catch performance regressions before deployment
- Validate scalability claims
- Enable confident deployments with known performance characteristics
- Support realistic capacity planning

Usage:
    # Basic run with 100 users
    locust -f realistic_workload.py --users 100 --spawn-rate 10 --run-time 30m

    # Run with custom host
    locust -f realistic_workload.py --host http://localhost:19530 --users 100

    # Web UI mode (default)
    locust -f realistic_workload.py

    # Headless mode with CSV output
    locust -f realistic_workload.py --headless --users 100 --spawn-rate 10 \
           --run-time 30m --csv=results
"""

import logging
import os
import random
import time
from typing import List, Optional

import numpy as np
from locust import User, task, between, events
from pymilvus import MilvusClient, DataType, Collection, connections

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


class LoadTestConfig:
    """Configuration for load testing parameters"""

    # Milvus connection settings
    MILVUS_HOST = os.getenv("MILVUS_HOST", "localhost")
    MILVUS_PORT = int(os.getenv("MILVUS_PORT", "19530"))
    MILVUS_URI = os.getenv("MILVUS_URI", f"http://{MILVUS_HOST}:{MILVUS_PORT}")

    # Collection settings
    COLLECTION_NAME = os.getenv("LOAD_TEST_COLLECTION", "load_test_collection")
    DIMENSION = int(os.getenv("VECTOR_DIMENSION", "768"))

    # Query distribution (should sum to 100)
    COMMON_QUERY_WEIGHT = int(os.getenv("COMMON_QUERY_WEIGHT", "70"))
    FILTERED_QUERY_WEIGHT = int(os.getenv("FILTERED_QUERY_WEIGHT", "20"))
    RARE_QUERY_WEIGHT = int(os.getenv("RARE_QUERY_WEIGHT", "10"))

    # Query parameters
    COMMON_QUERY_LIMIT = int(os.getenv("COMMON_QUERY_LIMIT", "10"))
    FILTERED_QUERY_LIMIT = int(os.getenv("FILTERED_QUERY_LIMIT", "10"))
    RARE_QUERY_LIMIT = int(os.getenv("RARE_QUERY_LIMIT", "100"))

    # Zipfian distribution parameter (higher = more skewed toward popular items)
    ZIPF_PARAMETER = float(os.getenv("ZIPF_PARAMETER", "1.5"))

    # Performance targets
    TARGET_QPS = int(os.getenv("TARGET_QPS", "1000"))
    TARGET_P50_MS = int(os.getenv("TARGET_P50_MS", "30"))
    TARGET_P95_MS = int(os.getenv("TARGET_P95_MS", "70"))
    TARGET_P99_MS = int(os.getenv("TARGET_P99_MS", "120"))
    TARGET_ERROR_RATE = float(os.getenv("TARGET_ERROR_RATE", "0.001"))  # 0.1%


class VectorGenerator:
    """Helper class for generating test vectors"""

    def __init__(self, dimension: int):
        self.dimension = dimension
        self._hot_vectors = self._generate_hot_vectors()

    def _generate_hot_vectors(self, num_hot: int = 1000) -> List[List[float]]:
        """Pre-generate hot vectors for common queries"""
        logger.info(f"Pre-generating {num_hot} hot vectors for common queries")
        vectors = []
        for _ in range(num_hot):
            vector = np.random.randn(self.dimension).astype(np.float32)
            # Normalize to unit length
            vector = vector / np.linalg.norm(vector)
            vectors.append(vector.tolist())
        return vectors

    def get_hot_vector(self, query_id: int) -> List[float]:
        """Get a hot vector using Zipfian distribution"""
        # Use modulo to wrap around if query_id exceeds hot vectors
        idx = query_id % len(self._hot_vectors)
        return self._hot_vectors[idx]

    def get_random_vector(self) -> List[float]:
        """Generate a random normalized vector"""
        vector = np.random.randn(self.dimension).astype(np.float32)
        vector = vector / np.linalg.norm(vector)
        return vector.tolist()


class FilterGenerator:
    """Helper class for generating realistic filter expressions"""

    CATEGORIES = ["electronics", "clothing", "food", "books", "toys", "sports"]
    PRICE_RANGES = [(0, 50), (50, 100), (100, 500), (500, 1000), (1000, 5000)]

    @staticmethod
    def random_filter() -> str:
        """Generate a random filter expression"""
        filter_type = random.choice(["category", "price", "combined"])

        if filter_type == "category":
            category = random.choice(FilterGenerator.CATEGORIES)
            return f'category == "{category}"'

        elif filter_type == "price":
            min_price, max_price = random.choice(FilterGenerator.PRICE_RANGES)
            return f"price >= {min_price} and price < {max_price}"

        else:  # combined
            category = random.choice(FilterGenerator.CATEGORIES)
            min_price, max_price = random.choice(FilterGenerator.PRICE_RANGES)
            return f'category == "{category}" and price >= {min_price} and price < {max_price}'


class MilvusLoadTestUser(User):
    """
    Locust user that simulates realistic Milvus query patterns.

    This user simulates three types of queries following realistic distributions:
    1. Common queries (70%): Hot queries following Zipfian distribution
    2. Filtered queries (20%): Queries with metadata filters
    3. Rare queries (10%): Long-tail queries with larger result sets
    """

    # Wait time between tasks (simulates realistic think time)
    wait_time = between(0.1, 0.5)

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.client = None
        self.vector_generator = VectorGenerator(LoadTestConfig.DIMENSION)
        self.filter_generator = FilterGenerator()

    def on_start(self):
        """Called when a user starts - initialize Milvus client"""
        try:
            logger.info(f"Connecting to Milvus at {LoadTestConfig.MILVUS_URI}")
            self.client = MilvusClient(uri=LoadTestConfig.MILVUS_URI)

            # Verify collection exists
            if not self._collection_exists():
                logger.warning(
                    f"Collection '{LoadTestConfig.COLLECTION_NAME}' does not exist. "
                    f"Please create it before running the load test."
                )
        except Exception as e:
            logger.error(f"Failed to connect to Milvus: {e}")
            raise

    def _collection_exists(self) -> bool:
        """Check if the test collection exists"""
        try:
            collections = self.client.list_collections()
            return LoadTestConfig.COLLECTION_NAME in collections
        except Exception as e:
            logger.error(f"Failed to check collection existence: {e}")
            return False

    @task(LoadTestConfig.COMMON_QUERY_WEIGHT)
    def search_common_queries(self):
        """
        Simulate hot queries using Zipfian distribution (80/20 rule).

        This represents the most common query pattern where a small set of
        queries account for the majority of traffic.
        """
        start_time = time.time()

        try:
            # Use Zipfian distribution for realistic access patterns
            # Lower query_ids will be accessed more frequently
            query_id = int(np.random.zipf(LoadTestConfig.ZIPF_PARAMETER))
            vector = self.vector_generator.get_hot_vector(query_id)

            result = self.client.search(
                collection_name=LoadTestConfig.COLLECTION_NAME,
                data=[vector],
                limit=LoadTestConfig.COMMON_QUERY_LIMIT,
                output_fields=["id"]
            )

            # Record success
            total_time = int((time.time() - start_time) * 1000)
            events.request.fire(
                request_type="search",
                name="common_query",
                response_time=total_time,
                response_length=len(result[0]) if result else 0,
                exception=None,
                context={}
            )

        except Exception as e:
            total_time = int((time.time() - start_time) * 1000)
            events.request.fire(
                request_type="search",
                name="common_query",
                response_time=total_time,
                response_length=0,
                exception=e,
                context={}
            )
            logger.error(f"Common query failed: {e}")

    @task(LoadTestConfig.FILTERED_QUERY_WEIGHT)
    def search_with_filter(self):
        """
        Simulate filtered queries with metadata conditions.

        This represents queries that combine vector search with scalar filtering,
        which is common in production e-commerce and recommendation scenarios.
        """
        start_time = time.time()

        try:
            vector = self.vector_generator.get_random_vector()
            filter_expr = self.filter_generator.random_filter()

            result = self.client.search(
                collection_name=LoadTestConfig.COLLECTION_NAME,
                data=[vector],
                filter=filter_expr,
                limit=LoadTestConfig.FILTERED_QUERY_LIMIT,
                output_fields=["id"]
            )

            # Record success
            total_time = int((time.time() - start_time) * 1000)
            events.request.fire(
                request_type="search",
                name="filtered_query",
                response_time=total_time,
                response_length=len(result[0]) if result else 0,
                exception=None,
                context={}
            )

        except Exception as e:
            total_time = int((time.time() - start_time) * 1000)
            events.request.fire(
                request_type="search",
                name="filtered_query",
                response_time=total_time,
                response_length=0,
                exception=e,
                context={}
            )
            logger.error(f"Filtered query failed: {e}")

    @task(LoadTestConfig.RARE_QUERY_WEIGHT)
    def search_rare_queries(self):
        """
        Simulate long-tail queries with larger result sets.

        This represents less frequent queries that might request more results,
        testing the system's ability to handle diverse query patterns.
        """
        start_time = time.time()

        try:
            vector = self.vector_generator.get_random_vector()

            result = self.client.search(
                collection_name=LoadTestConfig.COLLECTION_NAME,
                data=[vector],
                limit=LoadTestConfig.RARE_QUERY_LIMIT,
                output_fields=["id"]
            )

            # Record success
            total_time = int((time.time() - start_time) * 1000)
            events.request.fire(
                request_type="search",
                name="rare_query",
                response_time=total_time,
                response_length=len(result[0]) if result else 0,
                exception=None,
                context={}
            )

        except Exception as e:
            total_time = int((time.time() - start_time) * 1000)
            events.request.fire(
                request_type="search",
                name="rare_query",
                response_time=total_time,
                response_length=0,
                exception=e,
                context={}
            )
            logger.error(f"Rare query failed: {e}")


# Event handlers for test lifecycle
@events.test_start.add_listener
def on_test_start(environment, **kwargs):
    """Called when the test starts"""
    logger.info("=" * 80)
    logger.info("Starting Milvus Load Test")
    logger.info("=" * 80)
    logger.info(f"Configuration:")
    logger.info(f"  Milvus URI: {LoadTestConfig.MILVUS_URI}")
    logger.info(f"  Collection: {LoadTestConfig.COLLECTION_NAME}")
    logger.info(f"  Vector Dimension: {LoadTestConfig.DIMENSION}")
    logger.info(f"  Query Distribution: Common={LoadTestConfig.COMMON_QUERY_WEIGHT}%, "
                f"Filtered={LoadTestConfig.FILTERED_QUERY_WEIGHT}%, "
                f"Rare={LoadTestConfig.RARE_QUERY_WEIGHT}%")
    logger.info(f"Performance Targets:")
    logger.info(f"  Target QPS: {LoadTestConfig.TARGET_QPS}")
    logger.info(f"  Target P50: <{LoadTestConfig.TARGET_P50_MS}ms")
    logger.info(f"  Target P95: <{LoadTestConfig.TARGET_P95_MS}ms")
    logger.info(f"  Target P99: <{LoadTestConfig.TARGET_P99_MS}ms")
    logger.info(f"  Target Error Rate: <{LoadTestConfig.TARGET_ERROR_RATE * 100}%")
    logger.info("=" * 80)


@events.test_stop.add_listener
def on_test_stop(environment, **kwargs):
    """Called when the test stops - print summary and check against targets"""
    logger.info("=" * 80)
    logger.info("Load Test Completed")
    logger.info("=" * 80)

    # Get statistics
    stats = environment.stats

    if stats.total.num_requests > 0:
        logger.info("Results Summary:")
        logger.info(f"  Total Requests: {stats.total.num_requests}")
        logger.info(f"  Total Failures: {stats.total.num_failures}")
        logger.info(f"  Error Rate: {stats.total.fail_ratio * 100:.2f}%")
        logger.info(f"  Average Response Time: {stats.total.avg_response_time:.2f}ms")
        logger.info(f"  Median Response Time (P50): {stats.total.median_response_time:.2f}ms")
        logger.info(f"  95th Percentile (P95): {stats.total.get_response_time_percentile(0.95):.2f}ms")
        logger.info(f"  99th Percentile (P99): {stats.total.get_response_time_percentile(0.99):.2f}ms")
        logger.info(f"  Requests/sec: {stats.total.total_rps:.2f}")

        # Check against targets
        logger.info("\nTarget Validation:")

        # Check QPS
        qps_status = "✓" if stats.total.total_rps >= LoadTestConfig.TARGET_QPS else "✗"
        logger.info(f"  QPS: {stats.total.total_rps:.0f} "
                    f"(target: {LoadTestConfig.TARGET_QPS}) {qps_status}")

        # Check P50
        p50 = stats.total.median_response_time
        p50_status = "✓" if p50 <= LoadTestConfig.TARGET_P50_MS else "✗"
        logger.info(f"  P50: {p50:.0f}ms "
                    f"(target: <{LoadTestConfig.TARGET_P50_MS}ms) {p50_status}")

        # Check P95
        p95 = stats.total.get_response_time_percentile(0.95)
        p95_status = "✓" if p95 <= LoadTestConfig.TARGET_P95_MS else "✗"
        logger.info(f"  P95: {p95:.0f}ms "
                    f"(target: <{LoadTestConfig.TARGET_P95_MS}ms) {p95_status}")

        # Check P99
        p99 = stats.total.get_response_time_percentile(0.99)
        p99_status = "✓" if p99 <= LoadTestConfig.TARGET_P99_MS else "✗"
        logger.info(f"  P99: {p99:.0f}ms "
                    f"(target: <{LoadTestConfig.TARGET_P99_MS}ms) {p99_status}")

        # Check error rate
        error_rate = stats.total.fail_ratio
        error_status = "✓" if error_rate <= LoadTestConfig.TARGET_ERROR_RATE else "✗"
        logger.info(f"  Error rate: {error_rate * 100:.2f}% "
                    f"(target: <{LoadTestConfig.TARGET_ERROR_RATE * 100}%) {error_status}")

    logger.info("=" * 80)


if __name__ == "__main__":
    # This allows the script to be run directly for testing
    import sys
    logger.info("To run the load test, use: locust -f realistic_workload.py")
    logger.info("For help, use: locust -f realistic_workload.py --help")
