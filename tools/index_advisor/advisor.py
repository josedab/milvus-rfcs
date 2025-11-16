#!/usr/bin/env python3
"""
Milvus Index Advisor

Recommends optimal index configuration based on workload requirements.
"""

import math
from dataclasses import dataclass
from typing import Dict, List, Optional
from enum import Enum


class IndexType(Enum):
    FLAT = "FLAT"
    HNSW = "HNSW"
    IVF_FLAT = "IVF_FLAT"
    IVF_SQ8 = "IVF_SQ8"
    IVF_PQ = "IVF_PQ"
    DISK_ANN = "DiskANN"
    GPU_IVF_FLAT = "GPU_IVF_FLAT"
    GPU_IVF_PQ = "GPU_IVF_PQ"


class UseCase(Enum):
    RAG = "RAG/QA System"
    SIMILARITY = "Similarity Search"
    RECOMMENDATION = "Recommendation Engine"
    IMAGE_SEARCH = "Image Search"
    ECOMMERCE = "E-commerce Search"


@dataclass
class Recommendation:
    """Index recommendation with parameters and expected performance"""
    index_type: IndexType
    params: Dict[str, any]

    # Estimated performance
    memory_gb: float
    build_time_min: float
    query_latency_p95: float
    recall_at_10: float

    # Reasoning
    reason: str
    confidence: float  # 0.0-1.0

    # Alternatives
    alternatives: List['Recommendation']


class IndexAdvisor:
    """
    Main recommendation engine

    Uses decision tree + performance models to recommend optimal index.
    """

    def __init__(self):
        # Performance models (from empirical benchmarks)
        self.memory_models = self._load_memory_models()
        self.latency_models = self._load_latency_models()
        self.build_models = self._load_build_models()

    def recommend(
        self,
        num_vectors: int,
        dimensions: int,
        latency_requirement_ms: float,
        memory_budget_gb: float,
        qps_target: int,
        use_case: str = "general",
        has_gpu: bool = False,
    ) -> Recommendation:
        """
        Generate index recommendation based on requirements

        Args:
            num_vectors: Total number of vectors to index
            dimensions: Vector dimensionality
            latency_requirement_ms: P95 latency requirement
            memory_budget_gb: Available memory per QueryNode
            qps_target: Expected queries per second
            use_case: Workload type (RAG, similarity, etc.)
            has_gpu: Whether GPUs are available

        Returns:
            Recommendation with index type, params, and estimates
        """

        # Decision tree (6 rules)

        # Rule 1: Tiny datasets (<10k) → FLAT
        if num_vectors < 10_000:
            return self._recommend_flat(num_vectors, dimensions)

        # Rule 2: Billion-scale (>100M) → DiskANN
        if num_vectors > 100_000_000:
            return self._recommend_diskann(
                num_vectors, dimensions, latency_requirement_ms
            )

        # Rule 3: Low latency + sufficient memory → HNSW
        hnsw_memory = self._estimate_hnsw_memory(num_vectors, dimensions)
        if (latency_requirement_ms < 30 and
            hnsw_memory < memory_budget_gb * 0.8):
            return self._recommend_hnsw(num_vectors, dimensions)

        # Rule 4: High QPS + GPU available → GPU IVF
        if qps_target > 5000 and has_gpu:
            return self._recommend_gpu_ivf(
                num_vectors, dimensions, memory_budget_gb
            )

        # Rule 5: Memory constrained → IVF_PQ
        if memory_budget_gb < hnsw_memory * 0.5:
            return self._recommend_ivf_pq(num_vectors, dimensions)

        # Rule 6: Default balanced → IVF_FLAT or IVF_SQ8
        if memory_budget_gb < hnsw_memory * 0.7:
            return self._recommend_ivf_sq8(num_vectors, dimensions)
        else:
            return self._recommend_ivf_flat(num_vectors, dimensions)

    def _recommend_hnsw(
        self,
        num_vectors: int,
        dimensions: int
    ) -> Recommendation:
        """Recommend HNSW with optimal parameters"""

        # Parameter calculation
        M = self._calculate_hnsw_m(num_vectors)
        ef_construction = M * 15  # Rule of thumb: efC = M * 15
        ef = M * 4  # Search ef

        # Performance estimates
        memory_gb = self._estimate_hnsw_memory(num_vectors, dimensions, M)
        build_time_min = self._estimate_build_time(
            num_vectors, dimensions, "HNSW", {"M": M}
        )
        latency_p95 = self._estimate_latency(
            num_vectors, "HNSW", {"ef": ef}
        )
        recall = 0.95  # HNSW typically achieves 95%+ recall

        # Alternative recommendations
        alternatives = [
            self._recommend_ivf_flat(num_vectors, dimensions),
            self._recommend_diskann(num_vectors, dimensions, 100),
        ]

        return Recommendation(
            index_type=IndexType.HNSW,
            params={
                "M": M,
                "efConstruction": ef_construction,
                "ef": ef,
            },
            memory_gb=memory_gb,
            build_time_min=build_time_min,
            query_latency_p95=latency_p95,
            recall_at_10=recall,
            reason="Low latency requirement (<30ms) with sufficient memory",
            confidence=0.92,
            alternatives=alternatives,
        )

    def _recommend_ivf_flat(
        self,
        num_vectors: int,
        dimensions: int
    ) -> Recommendation:
        """Recommend IVF_FLAT with optimal parameters"""

        # Optimal nlist: sqrt(N) for balanced recall/speed
        nlist = int(math.sqrt(num_vectors))
        nlist = max(128, min(nlist, 16384))  # Clamp to reasonable range

        # Search nprobe: ~10% of nlist for good recall
        nprobe = max(16, int(nlist * 0.1))

        # Performance estimates
        memory_gb = num_vectors * dimensions * 4 / (1024**3) * 1.1  # +10% overhead
        build_time_min = num_vectors / 100000 * 2  # ~2 min per 100k vectors
        latency_p95 = 10 + (num_vectors / 1_000_000) * 5  # Scales with N
        recall = 0.98  # IVF_FLAT high recall

        alternatives = [
            self._recommend_hnsw(num_vectors, dimensions),
            self._recommend_ivf_sq8(num_vectors, dimensions),
        ]

        return Recommendation(
            index_type=IndexType.IVF_FLAT,
            params={
                "nlist": nlist,
                "nprobe": nprobe,
            },
            memory_gb=memory_gb,
            build_time_min=build_time_min,
            query_latency_p95=latency_p95,
            recall_at_10=recall,
            reason="Balanced performance and memory usage",
            confidence=0.88,
            alternatives=alternatives,
        )

    def _recommend_ivf_pq(
        self,
        num_vectors: int,
        dimensions: int
    ) -> Recommendation:
        """Recommend IVF_PQ for memory-constrained scenarios"""

        # Parameters
        nlist = int(math.sqrt(num_vectors))
        nlist = max(128, min(nlist, 16384))

        # PQ parameters
        m = self._calculate_pq_m(dimensions)  # Subvector count
        nbits = 8  # Standard: 8 bits per subvector

        nprobe = max(32, int(nlist * 0.15))  # Higher nprobe for PQ

        # Memory: Compressed to ~1/4 of original
        memory_gb = num_vectors * dimensions * 4 / (1024**3) * 0.25
        build_time_min = num_vectors / 80000 * 3  # Slower build than IVF_FLAT
        latency_p95 = 15 + (num_vectors / 1_000_000) * 6
        recall = 0.90  # Lower recall due to compression

        alternatives = [
            self._recommend_ivf_sq8(num_vectors, dimensions),
            self._recommend_diskann(num_vectors, dimensions, 100),
        ]

        return Recommendation(
            index_type=IndexType.IVF_PQ,
            params={
                "nlist": nlist,
                "m": m,
                "nbits": nbits,
                "nprobe": nprobe,
            },
            memory_gb=memory_gb,
            build_time_min=build_time_min,
            query_latency_p95=latency_p95,
            recall_at_10=recall,
            reason=f"Memory budget ({memory_gb:.1f}GB) requires compression",
            confidence=0.85,
            alternatives=alternatives,
        )

    def _recommend_diskann(
        self,
        num_vectors: int,
        dimensions: int,
        latency_requirement_ms: float
    ) -> Recommendation:
        """Recommend DiskANN for billion-scale datasets"""

        # DiskANN parameters
        search_list_size = int(latency_requirement_ms / 2)  # Rough heuristic

        # Memory: Only index structure, vectors on disk
        memory_gb = num_vectors * 100 / (1024**3)  # ~100 bytes per vector
        build_time_min = num_vectors / 50000 * 5  # Slower build
        latency_p95 = latency_requirement_ms * 1.5  # Conservative estimate
        recall = 0.92

        return Recommendation(
            index_type=IndexType.DISK_ANN,
            params={
                "search_list_size": search_list_size,
            },
            memory_gb=memory_gb,
            build_time_min=build_time_min,
            query_latency_p95=latency_p95,
            recall_at_10=recall,
            reason=f"Billion-scale dataset ({num_vectors:,} vectors)",
            confidence=0.90,
            alternatives=[],
        )

    def _recommend_flat(
        self,
        num_vectors: int,
        dimensions: int
    ) -> Recommendation:
        """Recommend FLAT for tiny datasets"""

        memory_gb = num_vectors * dimensions * 4 / (1024**3)
        build_time_min = 0.1  # Nearly instant
        latency_p95 = 5  # Very fast for small datasets
        recall = 1.0  # Perfect recall (brute force)

        return Recommendation(
            index_type=IndexType.FLAT,
            params={},
            memory_gb=memory_gb,
            build_time_min=build_time_min,
            query_latency_p95=latency_p95,
            recall_at_10=recall,
            reason=f"Small dataset ({num_vectors:,} vectors) - brute force optimal",
            confidence=0.99,
            alternatives=[],
        )

    # Helper methods

    def _calculate_hnsw_m(self, num_vectors: int) -> int:
        """Calculate optimal M parameter for HNSW"""
        if num_vectors < 100_000:
            return 8
        elif num_vectors < 1_000_000:
            return 16
        elif num_vectors < 10_000_000:
            return 24
        else:
            return 32

    def _calculate_pq_m(self, dimensions: int) -> int:
        """Calculate PQ subvector count"""
        # m must divide dimensions
        for m in [32, 16, 8, 4]:
            if dimensions % m == 0:
                return m
        return 8  # Fallback

    def _estimate_hnsw_memory(
        self,
        num_vectors: int,
        dimensions: int,
        M: int = 16
    ) -> float:
        """Estimate HNSW memory usage in GB"""
        # Formula: N * (dims * 4 + M * 2 * 1.2) bytes
        bytes_per_vector = dimensions * 4 + M * 2 * 1.2
        total_bytes = num_vectors * bytes_per_vector
        return total_bytes / (1024**3)

    def _estimate_build_time(
        self,
        num_vectors: int,
        dimensions: int,
        index_type: str,
        params: dict
    ) -> float:
        """Estimate index build time in minutes"""
        # Simplified model (actual would be more sophisticated)
        base_time = num_vectors / 100000  # 1 min per 100k vectors baseline

        if index_type == "HNSW":
            # HNSW slower due to graph construction
            return base_time * 3 * (params.get("M", 16) / 16)
        elif index_type == "IVF_FLAT":
            return base_time * 2
        elif index_type == "IVF_PQ":
            return base_time * 3
        else:
            return base_time

    def _estimate_latency(
        self,
        num_vectors: int,
        index_type: str,
        params: dict
    ) -> float:
        """Estimate P95 query latency in milliseconds"""
        # Simplified model
        if index_type == "HNSW":
            ef = params.get("ef", 64)
            return 10 + math.log10(num_vectors) * 5 + (ef / 64) * 10
        elif index_type == "IVF_FLAT":
            nprobe = params.get("nprobe", 32)
            return 15 + (nprobe / 32) * 20
        else:
            return 20

    def _load_memory_models(self):
        # Load empirical memory models
        return {}

    def _load_latency_models(self):
        # Load empirical latency models
        return {}

    def _load_build_models(self):
        # Load empirical build time models
        return {}

    def _recommend_ivf_sq8(self, num_vectors, dimensions):
        # Similar to IVF_FLAT but with scalar quantization
        nlist = int(math.sqrt(num_vectors))
        return Recommendation(
            index_type=IndexType.IVF_SQ8,
            params={"nlist": nlist, "nprobe": max(16, int(nlist * 0.1))},
            memory_gb=num_vectors * dimensions * 4 / (1024**3) * 0.6,
            build_time_min=num_vectors / 90000 * 2.5,
            query_latency_p95=12 + (num_vectors / 1_000_000) * 5.5,
            recall_at_10=0.95,
            reason="Good balance of memory and accuracy",
            confidence=0.87,
            alternatives=[],
        )

    def _recommend_gpu_ivf(self, num_vectors, dimensions, memory_budget_gb):
        return Recommendation(
            index_type=IndexType.GPU_IVF_FLAT,
            params={"nlist": int(math.sqrt(num_vectors)), "nprobe": 64},
            memory_gb=num_vectors * dimensions * 4 / (1024**3) * 1.2,
            build_time_min=num_vectors / 500000,  # GPU much faster
            query_latency_p95=5,  # GPU very fast
            recall_at_10=0.98,
            reason="High QPS requirement with GPU available",
            confidence=0.91,
            alternatives=[],
        )
