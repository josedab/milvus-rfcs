# RFC-0011: Migration Tool from Pinecone/Weaviate/Qdrant

**Status:** Proposed  
**Author:** Jose David Baena  
**Created:** 2025-04-03  
**Category:** Developer Experience  
**Priority:** Medium  
**Complexity:** High (6-8 weeks)  
**POC Status:** Designed, not implemented

## Summary

Create a unified migration CLI tool that automates data migration from Pinecone, Weaviate, and Qdrant to Milvus. Current migration requires 1-2 weeks of manual scripting, schema transformation, and validation. This tool reduces migration time to hours with automated schema mapping, index configuration translation, and recall validation.

**Expected Impact:**
- 10x faster migrations (hours vs weeks)
- Lower adoption barrier for users on competing platforms
- Better competitive positioning vs managed services
- Automated validation ensures migration quality

## Motivation

### Problem Statement

**Current migration process:**
1. Export data from source (manual scripting, hours)
2. Transform schema (error-prone, requires deep knowledge)
3. Map index configurations (Pinecone → Milvus parameter translation)
4. Import to Milvus (write custom code)
5. Validate results (no automated recall comparison)

**Time:** 1-2 weeks of engineering work for medium-scale migration.

### Use Cases

**Use Case 1: Startup Cost Reduction**
- Moving from Pinecone ($840/month) to Milvus ($540/month)
- Needs confidence in migration quality
- Can't afford downtime or data loss

**Use Case 2: Enterprise On-Prem Requirement**
- Compliance requires on-premise deployment
- Currently on managed Weaviate
- Need seamless migration path

**Use Case 3: Feature Parity Validation**
- Testing Milvus before committing
- Quick migration for POC
- Need to validate recall matches source

## Detailed Design

### CLI Interface

```bash
# Migration from Pinecone
milvus-migrate import \
    --source pinecone \
    --source-api-key $PINECONE_KEY \
    --source-index products \
    --source-environment us-east-1 \
    --dest-uri http://localhost:19530 \
    --dest-collection products \
    --batch-size 1000 \
    --with-metadata \
    --validate \
    --dry-run  # Preview before migrating

# Features:
# ✓ Automatic schema mapping
# ✓ Index configuration translation
# ✓ Progress tracking with resume
# ✓ Validation (recall comparison)
# ✓ Rollback on failure
# ✓ Cost estimation

# Output:
# ✓ Connected to Pinecone index 'products' (5,234,125 vectors)
# ✓ Analyzed schema: 3 metadata fields
#   - id (string) → VARCHAR
#   - category (string) → VARCHAR  
#   - price (number) → FLOAT
# ✓ Detected index config: Pinecone (unknown params)
#   Recommended Milvus config: HNSW (M=16, efConstruction=240)
# ✓ Estimated cost:
#   - Pinecone: $840/month (current)
#   - Milvus: $540/month (projected)
#   - Savings: $300/month (36%)
# 
# Migrating data: [===============>  ] 80% (4,187,300 / 5,234,125)
#   ETA: 6 minutes 23 seconds
#   Speed: 13,200 vectors/sec
#   Memory: 2.3 GB used
# 
# ✓ Migration complete in 28 minutes!
# ✓ Validation: 97.2% recall vs source (excellent)
# ✓ Latency: 28ms p95 (vs 32ms source - 12% faster!)
# ✓ Cost: $540/month (vs $840/month - 36% savings)
```

### Implementation

**Location:** `tools/milvus_migrate.py`

```python
#!/usr/bin/env python3
"""
Milvus Migration Tool

Migrate from Pinecone, Weaviate, Qdrant to Milvus
"""

import pinecone
import weaviate
from qdrant_client import QdrantClient
from pymilvus import MilvusClient, DataType

class MilvusMigrationTool:
    """Migrate from other vector databases to Milvus"""
    
    def migrate_from_pinecone(
        self,
        api_key: str,
        source_index: str,
        dest_uri: str,
        dest_collection: str,
        batch_size: int = 1000,
        validate: bool = True
    ):
        # 1. Connect to source
        pinecone.init(api_key=api_key)
        source = pinecone.Index(source_index)
        
        # 2. Analyze source schema
        stats = source.describe_index_stats()
        total_vectors = stats['total_vector_count']
        dimensions = stats['dimension']
        
        # 3. Map to Milvus schema
        milvus_schema = self._map_pinecone_schema(source, dimensions)
        
        # 4. Translate index config
        # Pinecone uses proprietary index (likely HNSW-based)
        # Recommend HNSW for Milvus
        milvus_index = {
            "index_type": "HNSW",
            "metric_type": "COSINE",
            "params": {"M": 16, "efConstruction": 240}
        }
        
        # 5. Create destination
        milvus = MilvusClient(uri=dest_uri)
        milvus.create_collection(dest_collection, schema=milvus_schema)
        milvus.create_index(dest_collection, "vector", milvus_index)
        
        # 6. Stream migration with progress
        migrated = 0
        for batch in self._fetch_pinecone_batches(source, batch_size):
            # Transform batch
            milvus_data = self._transform_pinecone_batch(batch)
            
            # Insert to Milvus
            milvus.insert(dest_collection, milvus_data)
            
            migrated += len(batch)
            print(f"Progress: {migrated}/{total_vectors} ({migrated/total_vectors*100:.1f}%)")
        
        # 7. Validate (optional)
        if validate:
            recall = self._validate_migration(source, milvus, dest_collection)
            print(f"Validation recall: {recall:.1%}")
        
        return {
            "total_vectors": total_vectors,
            "migrated": migrated,
            "validation_recall": recall if validate else None
        }
    
    def _map_pinecone_schema(self, source, dimensions):
        """Map Pinecone schema to Milvus schema"""
        # Pinecone uses flexible metadata
        # Map to Milvus JSON field + common fields
        
        schema = MilvusClient.create_schema(auto_id=False)
        schema.add_field("id", DataType.VARCHAR, max_length=100, is_primary=True)
        schema.add_field("vector", DataType.FLOAT_VECTOR, dim=dimensions)
        schema.add_field("metadata", DataType.JSON)  # All Pinecone metadata
        
        return schema
    
    def migrate_from_weaviate(self, ...):
        # Similar implementation for Weaviate
        pass
    
    def migrate_from_qdrant(self, ...):
        # Similar implementation for Qdrant
        pass
```

### Supported Sources

| Source | Schema Mapping | Index Translation | Validation |
|--------|---------------|-------------------|------------|
| Pinecone | ✅ Auto | ✅ HNSW default | ✅ Recall check |
| Weaviate | ✅ Auto | ✅ Based on config | ✅ Recall check |
| Qdrant | ✅ Auto | ✅ Based on config | ✅ Recall check |

## Expected Impact

- **10x faster migrations** (hours vs weeks)
- **Lower adoption barrier** from managed services
- **Automated validation** ensures quality
- **Cost savings** highlighted during migration

## Drawbacks

1. **Maintenance Burden** - must keep up with source API changes
2. **Limited Coverage** - may not handle all edge cases
3. **Validation Accuracy** - recall comparison has limitations

## References

- Blog Post: [`blog/posts/06_next_gen_improvements.md:590`](blog/posts/06_next_gen_improvements.md:590)

---

**Status:** Ready for prototyping - high value for user acquisition