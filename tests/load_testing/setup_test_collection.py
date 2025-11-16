#!/usr/bin/env python3
"""
Setup script for creating and populating Milvus test collections.

This script creates a collection with realistic data for load testing:
- Vector embeddings (normalized)
- Metadata fields (category, price, etc.)
- Realistic data distributions

Usage:
    # Create collection with 1M vectors (768 dimensions)
    python setup_test_collection.py --num-vectors 1000000 --dimension 768

    # Create smaller test collection
    python setup_test_collection.py --num-vectors 10000 --dimension 128

    # Drop existing collection first
    python setup_test_collection.py --drop-existing --num-vectors 100000
"""

import argparse
import logging
import random
import time
from typing import List

import numpy as np
from pymilvus import (
    MilvusClient,
    DataType,
    CollectionSchema,
    FieldSchema,
    Collection,
    connections,
    utility
)

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format='%(asctime)s - %(name)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)


class TestDataGenerator:
    """Generates realistic test data for load testing"""

    CATEGORIES = [
        "electronics", "clothing", "food", "books",
        "toys", "sports", "home", "beauty", "automotive", "garden"
    ]

    BRANDS = [
        "BrandA", "BrandB", "BrandC", "BrandD", "BrandE",
        "BrandF", "BrandG", "BrandH", "BrandI", "BrandJ"
    ]

    def __init__(self, dimension: int):
        self.dimension = dimension
        np.random.seed(42)  # Reproducible results

    def generate_vector(self) -> List[float]:
        """Generate a random normalized vector"""
        vector = np.random.randn(self.dimension).astype(np.float32)
        # Normalize to unit length
        vector = vector / np.linalg.norm(vector)
        return vector.tolist()

    def generate_category(self) -> str:
        """Generate a category with realistic distribution (some more popular)"""
        # Use weighted distribution - electronics and clothing more common
        weights = [0.2, 0.2, 0.1, 0.1, 0.1, 0.1, 0.05, 0.05, 0.05, 0.05]
        return random.choices(self.CATEGORIES, weights=weights)[0]

    def generate_price(self) -> float:
        """Generate a price with realistic distribution"""
        # Log-normal distribution for prices (more low-priced items)
        return round(float(np.random.lognormal(4, 1.5)), 2)

    def generate_brand(self) -> str:
        """Generate a brand name"""
        return random.choice(self.BRANDS)

    def generate_rating(self) -> float:
        """Generate a product rating (1-5 stars)"""
        # Beta distribution to simulate realistic ratings (skewed toward higher)
        return round(1 + 4 * np.random.beta(5, 2), 1)

    def generate_stock(self) -> int:
        """Generate stock quantity"""
        # Gamma distribution for stock levels
        return int(np.random.gamma(5, 10))

    def generate_batch(self, batch_size: int) -> dict:
        """Generate a batch of test data"""
        return {
            "vector": [self.generate_vector() for _ in range(batch_size)],
            "category": [self.generate_category() for _ in range(batch_size)],
            "price": [self.generate_price() for _ in range(batch_size)],
            "brand": [self.generate_brand() for _ in range(batch_size)],
            "rating": [self.generate_rating() for _ in range(batch_size)],
            "stock": [self.generate_stock() for _ in range(batch_size)],
        }


def create_collection_schema(collection_name: str, dimension: int) -> CollectionSchema:
    """Create collection schema with vector and metadata fields"""

    fields = [
        FieldSchema(
            name="id",
            dtype=DataType.INT64,
            is_primary=True,
            auto_id=True,
            description="Primary key"
        ),
        FieldSchema(
            name="vector",
            dtype=DataType.FLOAT_VECTOR,
            dim=dimension,
            description="Vector embedding"
        ),
        FieldSchema(
            name="category",
            dtype=DataType.VARCHAR,
            max_length=100,
            description="Product category"
        ),
        FieldSchema(
            name="price",
            dtype=DataType.FLOAT,
            description="Product price"
        ),
        FieldSchema(
            name="brand",
            dtype=DataType.VARCHAR,
            max_length=100,
            description="Brand name"
        ),
        FieldSchema(
            name="rating",
            dtype=DataType.FLOAT,
            description="Product rating (1-5)"
        ),
        FieldSchema(
            name="stock",
            dtype=DataType.INT64,
            description="Stock quantity"
        ),
    ]

    schema = CollectionSchema(
        fields=fields,
        description="Load testing collection with realistic product data"
    )

    return schema


def create_index(collection: Collection, dimension: int):
    """Create index on vector field"""
    logger.info("Creating index on vector field...")

    # Use IVF_FLAT for good balance of performance and accuracy
    # For production, consider IVF_PQ or HNSW based on requirements
    index_params = {
        "index_type": "IVF_FLAT",
        "metric_type": "L2",
        "params": {"nlist": 1024}
    }

    collection.create_index(
        field_name="vector",
        index_params=index_params
    )

    logger.info("Index created successfully")


def populate_collection(
    client: MilvusClient,
    collection_name: str,
    num_vectors: int,
    dimension: int,
    batch_size: int = 1000
):
    """Populate collection with test data"""

    logger.info(f"Populating collection with {num_vectors:,} vectors...")

    generator = TestDataGenerator(dimension)
    total_inserted = 0
    start_time = time.time()

    # Insert in batches
    num_batches = (num_vectors + batch_size - 1) // batch_size

    for batch_num in range(num_batches):
        current_batch_size = min(batch_size, num_vectors - total_inserted)

        # Generate batch data
        batch_data = generator.generate_batch(current_batch_size)

        # Insert batch
        client.insert(
            collection_name=collection_name,
            data=batch_data
        )

        total_inserted += current_batch_size

        # Progress update every 10 batches
        if (batch_num + 1) % 10 == 0 or total_inserted == num_vectors:
            elapsed = time.time() - start_time
            rate = total_inserted / elapsed if elapsed > 0 else 0
            logger.info(
                f"Progress: {total_inserted:,}/{num_vectors:,} vectors "
                f"({total_inserted/num_vectors*100:.1f}%) - "
                f"{rate:.0f} vectors/sec"
            )

    elapsed = time.time() - start_time
    logger.info(
        f"Inserted {total_inserted:,} vectors in {elapsed:.1f}s "
        f"({total_inserted/elapsed:.0f} vectors/sec)"
    )


def setup_test_collection(
    uri: str,
    collection_name: str,
    num_vectors: int,
    dimension: int,
    drop_existing: bool = False,
    batch_size: int = 1000
):
    """Main setup function"""

    logger.info("=" * 80)
    logger.info("Milvus Load Test Collection Setup")
    logger.info("=" * 80)
    logger.info(f"URI: {uri}")
    logger.info(f"Collection: {collection_name}")
    logger.info(f"Vectors: {num_vectors:,}")
    logger.info(f"Dimension: {dimension}")
    logger.info("=" * 80)

    # Connect to Milvus
    logger.info("Connecting to Milvus...")
    client = MilvusClient(uri=uri)

    # Check if collection exists
    collections = client.list_collections()
    collection_exists = collection_name in collections

    if collection_exists:
        if drop_existing:
            logger.info(f"Dropping existing collection '{collection_name}'...")
            client.drop_collection(collection_name)
            collection_exists = False
        else:
            logger.warning(
                f"Collection '{collection_name}' already exists. "
                f"Use --drop-existing to recreate it."
            )
            return

    # Create collection
    if not collection_exists:
        logger.info(f"Creating collection '{collection_name}'...")

        schema = create_collection_schema(collection_name, dimension)

        client.create_collection(
            collection_name=collection_name,
            schema=schema,
            consistency_level="Strong"
        )

        logger.info("Collection created successfully")

    # Get collection object for index creation
    connections.connect(uri=uri)
    collection = Collection(collection_name)

    # Create index
    create_index(collection, dimension)

    # Populate with data
    populate_collection(client, collection_name, num_vectors, dimension, batch_size)

    # Load collection
    logger.info("Loading collection into memory...")
    collection.load()
    logger.info("Collection loaded successfully")

    # Verify
    logger.info("Verifying collection...")
    num_entities = collection.num_entities
    logger.info(f"Total entities in collection: {num_entities:,}")

    logger.info("=" * 80)
    logger.info("Setup completed successfully!")
    logger.info("=" * 80)
    logger.info(f"Collection '{collection_name}' is ready for load testing")
    logger.info(f"Total vectors: {num_entities:,}")
    logger.info(f"Dimension: {dimension}")
    logger.info("")
    logger.info("Next steps:")
    logger.info("  1. Verify collection: ")
    logger.info(f"     from pymilvus import MilvusClient")
    logger.info(f"     client = MilvusClient(uri='{uri}')")
    logger.info(f"     print(client.describe_collection('{collection_name}'))")
    logger.info("")
    logger.info("  2. Run load test:")
    logger.info(f"     export LOAD_TEST_COLLECTION={collection_name}")
    logger.info(f"     export VECTOR_DIMENSION={dimension}")
    logger.info(f"     locust -f realistic_workload.py --users 100 --spawn-rate 10")
    logger.info("=" * 80)


def main():
    parser = argparse.ArgumentParser(
        description="Setup Milvus collection for load testing",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Create collection with 1M vectors (768 dimensions)
  python setup_test_collection.py --num-vectors 1000000 --dimension 768

  # Create smaller test collection
  python setup_test_collection.py --num-vectors 10000 --dimension 128

  # Drop existing collection first
  python setup_test_collection.py --drop-existing --num-vectors 100000

  # Use remote Milvus instance
  python setup_test_collection.py --uri http://milvus.example.com:19530 --num-vectors 1000000
        """
    )

    parser.add_argument(
        "--uri",
        type=str,
        default="http://localhost:19530",
        help="Milvus server URI (default: http://localhost:19530)"
    )

    parser.add_argument(
        "--collection-name",
        type=str,
        default="load_test_collection",
        help="Collection name (default: load_test_collection)"
    )

    parser.add_argument(
        "--num-vectors",
        type=int,
        default=100000,
        help="Number of vectors to generate (default: 100000)"
    )

    parser.add_argument(
        "--dimension",
        type=int,
        default=768,
        help="Vector dimension (default: 768)"
    )

    parser.add_argument(
        "--batch-size",
        type=int,
        default=1000,
        help="Batch size for insertion (default: 1000)"
    )

    parser.add_argument(
        "--drop-existing",
        action="store_true",
        help="Drop existing collection if it exists"
    )

    args = parser.parse_args()

    # Validate arguments
    if args.num_vectors <= 0:
        parser.error("num-vectors must be positive")

    if args.dimension <= 0:
        parser.error("dimension must be positive")

    if args.batch_size <= 0:
        parser.error("batch-size must be positive")

    try:
        setup_test_collection(
            uri=args.uri,
            collection_name=args.collection_name,
            num_vectors=args.num_vectors,
            dimension=args.dimension,
            drop_existing=args.drop_existing,
            batch_size=args.batch_size
        )
    except Exception as e:
        logger.error(f"Setup failed: {e}", exc_info=True)
        return 1

    return 0


if __name__ == "__main__":
    exit(main())
