#!/usr/bin/env python3
"""
Milvus Configuration Validation Framework

Pre-deployment configuration validation tool that catches misconfigurations
before they reach production. Validates parameter ranges, compatibility rules,
and resource requirements.

Usage:
    python tools/config_validator.py --config milvus.yaml --dry-run
    python tools/config_validator.py --validate-index HNSW --params '{"M": 32, "efConstruction": 500}' \
        --num-vectors 1000000 --dimensions 768 --memory-budget 64
"""

import argparse
import json
import sys
import yaml
from typing import Dict, List, Any, Optional, Tuple
import math


class ValidationResult:
    """Container for validation results"""

    def __init__(self):
        self.errors: List[str] = []
        self.warnings: List[str] = []

    def add_error(self, message: str):
        """Add an error message"""
        self.errors.append(message)

    def add_warning(self, message: str):
        """Add a warning message"""
        self.warnings.append(message)

    def has_errors(self) -> bool:
        """Check if there are any errors"""
        return len(self.errors) > 0

    def has_warnings(self) -> bool:
        """Check if there are any warnings"""
        return len(self.warnings) > 0

    def merge(self, other: 'ValidationResult'):
        """Merge another validation result into this one"""
        self.errors.extend(other.errors)
        self.warnings.extend(other.warnings)


class ConfigValidator:
    """Validate Milvus configuration before deployment"""

    # Constants from internal/util/indexparamcheck/constraints.go
    HNSW_MIN_M = 1
    HNSW_MAX_M = 2048
    HNSW_RECOMMENDED_MAX_M = 64
    HNSW_MIN_EF_CONSTRUCTION = 1
    HNSW_MAX_EF_CONSTRUCTION = 2147483647

    IVF_MIN_NLIST = 1
    IVF_MAX_NLIST = 65536

    # Memory estimation constants (bytes per element)
    FLOAT_VECTOR_BYTES_PER_DIM = 4
    HNSW_OVERHEAD_MULTIPLIER = 1.2

    SUPPORTED_METRIC_TYPES = {
        'float': ['L2', 'IP', 'COSINE'],
        'binary': ['HAMMING', 'JACCARD', 'SUBSTRUCTURE', 'SUPERSTRUCTURE'],
        'hnsw': ['L2', 'IP', 'COSINE']
    }

    def __init__(self):
        self.result = ValidationResult()

    def validate_index_config(
        self,
        index_type: str,
        params: Dict[str, Any],
        num_vectors: Optional[int] = None,
        dimensions: Optional[int] = None,
        memory_budget_gb: Optional[float] = None
    ) -> ValidationResult:
        """
        Validate index configuration parameters

        Args:
            index_type: Type of index (HNSW, IVF_FLAT, IVF_PQ, etc.)
            params: Index parameters dictionary
            num_vectors: Number of vectors in the collection
            dimensions: Vector dimensions
            memory_budget_gb: Available memory budget in GB

        Returns:
            ValidationResult containing errors and warnings
        """
        result = ValidationResult()

        index_type_upper = index_type.upper()

        if index_type_upper == "HNSW":
            result.merge(self._validate_hnsw(params, num_vectors, dimensions, memory_budget_gb))
        elif index_type_upper.startswith("IVF"):
            result.merge(self._validate_ivf(index_type_upper, params, num_vectors, dimensions, memory_budget_gb))
        elif index_type_upper in ["FLAT", "BIN_FLAT"]:
            result.merge(self._validate_flat(params, num_vectors, dimensions, memory_budget_gb))
        else:
            result.add_warning(f"Index type '{index_type}' validation not implemented yet")

        # Validate metric type if provided
        if 'metric_type' in params or 'metricType' in params:
            metric_type = params.get('metric_type') or params.get('metricType')
            result.merge(self._validate_metric_type(metric_type, index_type_upper))

        return result

    def _validate_hnsw(
        self,
        params: Dict[str, Any],
        num_vectors: Optional[int],
        dimensions: Optional[int],
        memory_budget_gb: Optional[float]
    ) -> ValidationResult:
        """Validate HNSW index parameters"""
        result = ValidationResult()

        # Get parameters (try both snake_case and camelCase)
        M = params.get("M", params.get("m", 16))
        efConstruction = params.get("efConstruction", params.get("ef_construction", 200))

        # Convert to int if string
        try:
            M = int(M)
            efConstruction = int(efConstruction)
        except (ValueError, TypeError) as e:
            result.add_error(f"Invalid parameter types: {e}")
            return result

        # Validation: M range (hard limits)
        if M < self.HNSW_MIN_M or M > self.HNSW_MAX_M:
            result.add_error(
                f"HNSW M={M} out of valid range "
                f"[{self.HNSW_MIN_M}, {self.HNSW_MAX_M}]"
            )

        # Validation: M range (recommended limits)
        if M > self.HNSW_RECOMMENDED_MAX_M:
            result.add_error(
                f"HNSW M={M} exceeds recommended maximum ({self.HNSW_RECOMMENDED_MAX_M}). "
                f"High M values can cause OOM errors."
            )

        # Validation: efConstruction range
        if efConstruction < self.HNSW_MIN_EF_CONSTRUCTION:
            result.add_error(
                f"HNSW efConstruction={efConstruction} below minimum "
                f"({self.HNSW_MIN_EF_CONSTRUCTION})"
            )
        elif efConstruction > self.HNSW_MAX_EF_CONSTRUCTION:
            result.add_error(
                f"HNSW efConstruction={efConstruction} exceeds maximum "
                f"({self.HNSW_MAX_EF_CONSTRUCTION})"
            )

        # Warning: efConstruction vs M ratio
        if efConstruction < M * 10:
            result.add_warning(
                f"HNSW efConstruction={efConstruction} should be ≥ {M * 10} "
                f"(10x M={M}) for good recall. Current ratio: {efConstruction/M:.1f}x"
            )

        # Warning: Very high efConstruction
        if efConstruction > 1000:
            result.add_warning(
                f"HNSW efConstruction={efConstruction} is very high. "
                f"This will slow down index building significantly."
            )

        # Memory estimation
        if num_vectors and dimensions and memory_budget_gb:
            result.merge(self._validate_hnsw_memory(
                M, num_vectors, dimensions, memory_budget_gb
            ))

        return result

    def _validate_hnsw_memory(
        self,
        M: int,
        num_vectors: int,
        dimensions: int,
        memory_budget_gb: float
    ) -> ValidationResult:
        """Validate HNSW memory requirements"""
        result = ValidationResult()

        # Memory estimation formula:
        # base_memory = num_vectors * dimensions * 4 (float32)
        # graph_memory = num_vectors * M * 2 * 8 (2*M edges, 8 bytes per edge)
        # overhead = 1.2x multiplier

        base_memory_bytes = num_vectors * dimensions * self.FLOAT_VECTOR_BYTES_PER_DIM
        graph_memory_bytes = num_vectors * M * 2 * 8  # 2*M edges per node
        total_memory_bytes = (base_memory_bytes + graph_memory_bytes) * self.HNSW_OVERHEAD_MULTIPLIER

        estimated_memory_gb = total_memory_bytes / (1024 ** 3)

        # Error if exceeds 90% of budget
        if estimated_memory_gb > memory_budget_gb * 0.9:
            result.add_error(
                f"Estimated HNSW memory {estimated_memory_gb:.1f}GB exceeds "
                f"90% of budget ({memory_budget_gb * 0.9:.1f}GB / {memory_budget_gb}GB total). "
                f"Consider reducing M or number of vectors."
            )
        # Warning if exceeds 75% of budget
        elif estimated_memory_gb > memory_budget_gb * 0.75:
            result.add_warning(
                f"Estimated HNSW memory {estimated_memory_gb:.1f}GB uses "
                f"{(estimated_memory_gb/memory_budget_gb)*100:.1f}% of budget ({memory_budget_gb}GB). "
                f"Consider monitoring memory usage closely."
            )

        return result

    def _validate_ivf(
        self,
        index_type: str,
        params: Dict[str, Any],
        num_vectors: Optional[int],
        dimensions: Optional[int],
        memory_budget_gb: Optional[float]
    ) -> ValidationResult:
        """Validate IVF-family index parameters"""
        result = ValidationResult()

        # Get nlist parameter
        nlist = params.get("nlist", params.get("nList", 1024))

        try:
            nlist = int(nlist)
        except (ValueError, TypeError) as e:
            result.add_error(f"Invalid nlist parameter: {e}")
            return result

        # Validation: nlist range
        if nlist < self.IVF_MIN_NLIST or nlist > self.IVF_MAX_NLIST:
            result.add_error(
                f"IVF nlist={nlist} out of valid range "
                f"[{self.IVF_MIN_NLIST}, {self.IVF_MAX_NLIST}]"
            )

        # Validation: nlist vs number of vectors
        if num_vectors:
            # Rule of thumb: nlist should be sqrt(num_vectors) to 4*sqrt(num_vectors)
            recommended_nlist_min = int(math.sqrt(num_vectors))
            recommended_nlist_max = int(4 * math.sqrt(num_vectors))

            if nlist < recommended_nlist_min:
                result.add_warning(
                    f"IVF nlist={nlist} is too low for {num_vectors:,} vectors. "
                    f"Recommended range: [{recommended_nlist_min}, {recommended_nlist_max}]. "
                    f"Low nlist may cause poor search performance."
                )
            elif nlist > recommended_nlist_max:
                result.add_warning(
                    f"IVF nlist={nlist} is too high for {num_vectors:,} vectors. "
                    f"Recommended range: [{recommended_nlist_min}, {recommended_nlist_max}]. "
                    f"High nlist may increase search latency."
                )

            # Warning: nlist > num_vectors
            if nlist > num_vectors:
                result.add_warning(
                    f"IVF nlist={nlist} exceeds number of vectors ({num_vectors:,}). "
                    f"This is inefficient and may cause issues."
                )

        # IVF_PQ specific validation
        if "PQ" in index_type:
            result.merge(self._validate_ivf_pq(params, dimensions))

        # Memory estimation for IVF_FLAT
        if index_type == "IVF_FLAT" and num_vectors and dimensions and memory_budget_gb:
            result.merge(self._validate_ivf_flat_memory(
                nlist, num_vectors, dimensions, memory_budget_gb
            ))

        return result

    def _validate_ivf_pq(
        self,
        params: Dict[str, Any],
        dimensions: Optional[int]
    ) -> ValidationResult:
        """Validate IVF_PQ specific parameters"""
        result = ValidationResult()

        m = params.get("m", params.get("M"))
        nbits = params.get("nbits", params.get("nBits", 8))

        if m is None:
            result.add_warning("IVF_PQ parameter 'm' (number of sub-vectors) not specified")
            return result

        try:
            m = int(m)
            nbits = int(nbits)
        except (ValueError, TypeError) as e:
            result.add_error(f"Invalid IVF_PQ parameters: {e}")
            return result

        # Validation: m must divide dimensions evenly
        if dimensions and dimensions % m != 0:
            result.add_error(
                f"IVF_PQ m={m} must divide dimensions ({dimensions}) evenly. "
                f"{dimensions} % {m} = {dimensions % m}"
            )

        # Validation: nbits range (typically 8 is used)
        if nbits not in [8, 16]:
            result.add_warning(
                f"IVF_PQ nbits={nbits} is non-standard. "
                f"Typical values are 8 or 16."
            )

        # Warning: Too many sub-vectors reduces quality
        if dimensions and m > dimensions / 2:
            result.add_warning(
                f"IVF_PQ m={m} is very high relative to dimensions ({dimensions}). "
                f"This may significantly reduce recall."
            )

        return result

    def _validate_ivf_flat_memory(
        self,
        nlist: int,
        num_vectors: int,
        dimensions: int,
        memory_budget_gb: float
    ) -> ValidationResult:
        """Validate IVF_FLAT memory requirements"""
        result = ValidationResult()

        # IVF_FLAT stores full vectors + centroid information
        vector_memory_bytes = num_vectors * dimensions * self.FLOAT_VECTOR_BYTES_PER_DIM
        centroid_memory_bytes = nlist * dimensions * self.FLOAT_VECTOR_BYTES_PER_DIM
        total_memory_bytes = (vector_memory_bytes + centroid_memory_bytes) * 1.1  # 10% overhead

        estimated_memory_gb = total_memory_bytes / (1024 ** 3)

        if estimated_memory_gb > memory_budget_gb * 0.9:
            result.add_error(
                f"Estimated IVF_FLAT memory {estimated_memory_gb:.1f}GB exceeds "
                f"90% of budget ({memory_budget_gb * 0.9:.1f}GB / {memory_budget_gb}GB total)"
            )
        elif estimated_memory_gb > memory_budget_gb * 0.75:
            result.add_warning(
                f"Estimated IVF_FLAT memory {estimated_memory_gb:.1f}GB uses "
                f"{(estimated_memory_gb/memory_budget_gb)*100:.1f}% of budget ({memory_budget_gb}GB)"
            )

        return result

    def _validate_flat(
        self,
        params: Dict[str, Any],
        num_vectors: Optional[int],
        dimensions: Optional[int],
        memory_budget_gb: Optional[float]
    ) -> ValidationResult:
        """Validate FLAT index (brute force)"""
        result = ValidationResult()

        # Warning: FLAT not recommended for large datasets
        if num_vectors and num_vectors > 1_000_000:
            result.add_warning(
                f"FLAT index with {num_vectors:,} vectors will use brute force search. "
                f"Consider using HNSW or IVF for better performance."
            )

        # Memory check
        if num_vectors and dimensions and memory_budget_gb:
            vector_memory_bytes = num_vectors * dimensions * self.FLOAT_VECTOR_BYTES_PER_DIM
            estimated_memory_gb = vector_memory_bytes / (1024 ** 3)

            if estimated_memory_gb > memory_budget_gb * 0.9:
                result.add_error(
                    f"Estimated FLAT memory {estimated_memory_gb:.1f}GB exceeds budget ({memory_budget_gb}GB)"
                )

        return result

    def _validate_metric_type(
        self,
        metric_type: str,
        index_type: str
    ) -> ValidationResult:
        """Validate metric type compatibility"""
        result = ValidationResult()

        metric_upper = metric_type.upper()

        # Check HNSW specific metrics
        if index_type == "HNSW":
            if metric_upper not in self.SUPPORTED_METRIC_TYPES['hnsw']:
                result.add_error(
                    f"Metric type '{metric_type}' not supported for HNSW. "
                    f"Supported: {self.SUPPORTED_METRIC_TYPES['hnsw']}"
                )

        return result

    def validate_config_file(self, config_path: str) -> ValidationResult:
        """
        Validate a Milvus configuration file

        Args:
            config_path: Path to milvus.yaml configuration file

        Returns:
            ValidationResult containing errors and warnings
        """
        result = ValidationResult()

        try:
            with open(config_path, 'r') as f:
                config = yaml.safe_load(f)
        except FileNotFoundError:
            result.add_error(f"Configuration file not found: {config_path}")
            return result
        except yaml.YAMLError as e:
            result.add_error(f"Invalid YAML in configuration file: {e}")
            return result

        # Validate various configuration sections
        # Note: This is a basic implementation. Extend based on actual config structure.

        if not config:
            result.add_warning("Configuration file is empty")
            return result

        # Add more specific validation based on milvus.yaml structure
        result.add_warning(
            "Full configuration file validation not yet implemented. "
            "Use --validate-index for index parameter validation."
        )

        return result


def print_results(result: ValidationResult, verbose: bool = False):
    """Pretty print validation results"""

    print("\n" + "="*60)
    print("  MILVUS CONFIGURATION VALIDATION RESULTS")
    print("="*60 + "\n")

    if not result.has_errors() and not result.has_warnings():
        print("✓ Validation passed! No errors or warnings found.\n")
        return 0

    if result.has_errors():
        print(f"❌ Errors found ({len(result.errors)}):\n")
        for i, error in enumerate(result.errors, 1):
            print(f"  {i}. {error}")
        print()

    if result.has_warnings():
        print(f"⚠️  Warnings ({len(result.warnings)}):\n")
        for i, warning in enumerate(result.warnings, 1):
            print(f"  {i}. {warning}")
        print()

    if result.has_errors():
        print("❌ Recommendation: Fix errors before deployment\n")
        return 1
    else:
        print("⚠️  Recommendation: Review warnings and adjust if needed\n")
        return 0


def main():
    """Main CLI entry point"""
    parser = argparse.ArgumentParser(
        description="Milvus Configuration Validation Framework",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Validate index configuration
  python tools/config_validator.py --validate-index HNSW \\
      --params '{"M": 32, "efConstruction": 500}' \\
      --num-vectors 1000000 --dimensions 768 --memory-budget 64

  # Validate IVF index
  python tools/config_validator.py --validate-index IVF_FLAT \\
      --params '{"nlist": 1024}' \\
      --num-vectors 10000000 --dimensions 512

  # Validate configuration file (future)
  python tools/config_validator.py --config configs/milvus.yaml --dry-run
        """
    )

    parser.add_argument(
        '--config',
        type=str,
        help='Path to milvus.yaml configuration file'
    )

    parser.add_argument(
        '--validate-index',
        type=str,
        help='Index type to validate (HNSW, IVF_FLAT, IVF_PQ, etc.)'
    )

    parser.add_argument(
        '--params',
        type=str,
        help='Index parameters as JSON string (e.g., \'{"M": 32, "efConstruction": 500}\')'
    )

    parser.add_argument(
        '--num-vectors',
        type=int,
        help='Number of vectors in the collection'
    )

    parser.add_argument(
        '--dimensions',
        type=int,
        help='Vector dimensions'
    )

    parser.add_argument(
        '--memory-budget',
        type=float,
        help='Available memory budget in GB'
    )

    parser.add_argument(
        '--dry-run',
        action='store_true',
        help='Perform validation without making changes'
    )

    parser.add_argument(
        '--verbose',
        action='store_true',
        help='Enable verbose output'
    )

    args = parser.parse_args()

    validator = ConfigValidator()
    result = ValidationResult()

    # Validate index configuration
    if args.validate_index:
        if not args.params:
            print("Error: --params required when using --validate-index")
            return 1

        try:
            params = json.loads(args.params)
        except json.JSONDecodeError as e:
            print(f"Error: Invalid JSON in --params: {e}")
            return 1

        result = validator.validate_index_config(
            index_type=args.validate_index,
            params=params,
            num_vectors=args.num_vectors,
            dimensions=args.dimensions,
            memory_budget_gb=args.memory_budget
        )

    # Validate configuration file
    elif args.config:
        result = validator.validate_config_file(args.config)

    else:
        parser.print_help()
        return 1

    # Print results
    return print_results(result, args.verbose)


if __name__ == "__main__":
    sys.exit(main())
