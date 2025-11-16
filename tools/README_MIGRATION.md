# Milvus Migration Tool

A unified CLI tool for migrating vector data from Pinecone, Weaviate, and Qdrant to Milvus.

## Features

- **Automated Schema Mapping**: Automatically detects and maps source schemas to Milvus
- **Index Configuration Translation**: Translates index configurations from source databases
- **Progress Tracking**: Real-time progress display with ETA
- **Resume Support**: Resume interrupted migrations from checkpoints
- **Validation**: Optional recall comparison to validate migration quality
- **Cost Estimation**: Estimates monthly cost savings when migrating to Milvus
- **Dry Run Mode**: Preview migration without executing
- **Batch Processing**: Configurable batch sizes for optimal performance

## Installation

### Prerequisites

- Python 3.8 or higher
- Access to source vector database (Pinecone, Weaviate, or Qdrant)
- Running Milvus instance

### Install Dependencies

```bash
pip install -r requirements-migrate.txt
```

Or install specific dependencies:

```bash
# For Pinecone migration
pip install pymilvus pinecone-client

# For Weaviate migration
pip install pymilvus weaviate-client

# For Qdrant migration
pip install pymilvus qdrant-client
```

## Usage

### Basic Migration from Pinecone

```bash
python milvus_migrate.py import \
    --source pinecone \
    --source-api-key $PINECONE_API_KEY \
    --source-index my-index \
    --dest-uri http://localhost:19530 \
    --dest-collection my-collection
```

### Migration from Weaviate

```bash
python milvus_migrate.py import \
    --source weaviate \
    --source-url http://localhost:8080 \
    --source-index Article \
    --dest-uri http://localhost:19530 \
    --dest-collection articles
```

### Migration from Qdrant

```bash
python milvus_migrate.py import \
    --source qdrant \
    --source-url http://localhost:6333 \
    --source-api-key $QDRANT_API_KEY \
    --source-index products \
    --dest-uri http://localhost:19530 \
    --dest-collection products
```

### Advanced Options

#### Dry Run (Preview Only)

Preview the migration without executing:

```bash
python milvus_migrate.py import \
    --source pinecone \
    --source-api-key $PINECONE_API_KEY \
    --source-index my-index \
    --dest-uri http://localhost:19530 \
    --dest-collection my-collection \
    --dry-run
```

#### With Validation

Validate migration quality with recall comparison:

```bash
python milvus_migrate.py import \
    --source pinecone \
    --source-api-key $PINECONE_API_KEY \
    --source-index my-index \
    --dest-uri http://localhost:19530 \
    --dest-collection my-collection \
    --validate
```

#### Resume Interrupted Migration

Resume from the last checkpoint:

```bash
python milvus_migrate.py import \
    --source pinecone \
    --source-api-key $PINECONE_API_KEY \
    --source-index my-index \
    --dest-uri http://localhost:19530 \
    --dest-collection my-collection \
    --resume
```

#### Custom Batch Size

Adjust batch size for performance tuning:

```bash
python milvus_migrate.py import \
    --source weaviate \
    --source-url http://localhost:8080 \
    --source-index Article \
    --dest-uri http://localhost:19530 \
    --dest-collection articles \
    --batch-size 500
```

#### Without Metadata

Migrate only vectors, excluding metadata:

```bash
python milvus_migrate.py import \
    --source qdrant \
    --source-url http://localhost:6333 \
    --source-index products \
    --dest-uri http://localhost:19530 \
    --dest-collection products \
    --no-metadata
```

## Command Line Arguments

### Required Arguments

- `command`: Command to execute (currently only `import` is supported)
- `--source`: Source database type (`pinecone`, `weaviate`, or `qdrant`)
- `--source-index`: Source index/collection/class name
- `--dest-uri`: Milvus connection URI
- `--dest-collection`: Destination Milvus collection name

### Source Configuration

- `--source-api-key`: API key for source database (required for Pinecone and Qdrant)
- `--source-url`: Source database URL (required for Weaviate and Qdrant)
- `--source-environment`: Source environment (for Pinecone)

### Destination Configuration

- `--dest-token`: Milvus authentication token (if required)

### Migration Options

- `--batch-size`: Number of vectors per batch (default: 1000)
- `--with-metadata`: Include metadata fields (default: enabled)
- `--no-metadata`: Exclude metadata fields
- `--validate`: Validate migration with recall comparison
- `--dry-run`: Preview migration without executing
- `--resume`: Resume from checkpoint
- `--checkpoint-file`: Custom checkpoint file path

## Example Output

```
Starting migration from pinecone to Milvus...
Connecting to Pinecone index 'products'...
✓ Connected to Pinecone index 'products' (5,234,125 vectors)
  Dimensions: 1536

Detecting metadata schema...
✓ Analyzed schema: 3 metadata fields
  - category (str) → VARCHAR
  - price (float) → FLOAT
  - description (str) → VARCHAR

Mapping to Milvus schema...
✓ Detected index config: Pinecone (proprietary)
  Recommended Milvus config: HNSW (M=16, efConstruction=240)

✓ Estimated costs:
  - Pinecone: ~$840.00/month (estimated)
  - Milvus: ~$540.00/month (estimated)
  - Savings: ~$300.00/month (36%)

Creating Milvus collection 'products'...
✓ Collection created

Migrating data...
======================================================================
Migrating: [===============>  ] 80.0% (4,187,300 / 5,234,125) | Speed: 13200 vec/s | ETA: 6.4 min

======================================================================
✓ Migration complete in 28.3 minutes!
  Total vectors: 5,234,125
  Migrated: 5,234,125

Validating migration...
✓ Validation: 97.2% recall vs source (excellent)

======================================================================
Migration Summary
======================================================================
Source: pinecone (products)
Destination: Milvus (products)
Vectors migrated: 5,234,125 / 5,234,125
Validation recall: 97.2%
Duration: 28.3 minutes
```

## Schema Mapping

### Pinecone to Milvus

- Vector ID → VARCHAR (primary key)
- Vector embeddings → FLOAT_VECTOR
- All metadata → JSON field

### Weaviate to Milvus

- Object UUID → VARCHAR (primary key)
- Vector embeddings → FLOAT_VECTOR
- Properties → JSON field

### Qdrant to Milvus

- Point ID → VARCHAR (primary key)
- Vector → FLOAT_VECTOR
- Payload → JSON field

## Index Configuration

The tool automatically recommends index configurations based on the source:

| Source | Milvus Index | Parameters |
|--------|--------------|------------|
| Pinecone | HNSW | M=16, efConstruction=240 |
| Weaviate | HNSW | M=16, efConstruction=240 |
| Qdrant | HNSW | M=16, efConstruction=240 |

## Resume and Checkpointing

The tool automatically saves checkpoints during migration. If interrupted:

1. The checkpoint file is saved as `.migration_checkpoint_<hash>.json`
2. Resume with `--resume` flag
3. The tool will continue from the last successful batch
4. Checkpoint is automatically removed after successful completion

## Validation

When using `--validate`, the tool:

1. Samples random query vectors
2. Queries both source and destination databases
3. Compares top-k results
4. Calculates recall percentage

Validation results:

- 95%+ recall: Excellent
- 90-95% recall: Good
- <90% recall: Needs review

## Troubleshooting

### Connection Errors

**Pinecone:**
```bash
# Verify API key
export PINECONE_API_KEY="your-api-key"

# Check index exists
pinecone list-indexes
```

**Weaviate:**
```bash
# Verify Weaviate is running
curl http://localhost:8080/v1/.well-known/ready
```

**Qdrant:**
```bash
# Verify Qdrant is running
curl http://localhost:6333/
```

### Memory Issues

If experiencing memory issues with large batches:

```bash
# Reduce batch size
--batch-size 100
```

### Network Timeouts

For slow networks or large vectors:

```bash
# Use smaller batches and resume support
--batch-size 500 --resume
```

## Limitations

1. **Pinecone Pagination**: Pinecone doesn't provide direct iteration over all vectors. For large datasets, consider exporting from Pinecone first.

2. **Metadata Complexity**: Complex nested metadata structures are stored as JSON in Milvus.

3. **Index Parameters**: The tool uses conservative default index parameters. Fine-tune based on your use case.

4. **Validation Sampling**: Validation uses a sample of queries, not exhaustive comparison.

## Performance Tips

1. **Batch Size**: Start with 1000, adjust based on vector dimensions and network speed
2. **Network**: Run the tool close to both source and destination for faster migration
3. **Resources**: Ensure Milvus has sufficient memory for index building
4. **Resume**: Enable resume for large migrations to handle interruptions

## Related Documentation

- [Milvus Documentation](https://milvus.io/docs)
- [RFC-0011: Migration Tool](../rfcs/0011-migration-tool.md)
- [Pinecone API Documentation](https://docs.pinecone.io/)
- [Weaviate Documentation](https://weaviate.io/developers/weaviate)
- [Qdrant Documentation](https://qdrant.tech/documentation/)

## Support

For issues and questions:

1. Check the troubleshooting section above
2. Review the RFC document: `rfcs/0011-migration-tool.md`
3. File an issue in the Milvus repository

## License

This tool is part of the Milvus project and follows the same license.
