#!/usr/bin/env python3
"""
Memory Over-Provisioning Detection Tool for Milvus

This tool analyzes memory usage patterns across QueryNodes and detects
over-provisioning opportunities to optimize infrastructure costs.

Based on RFC-0017: Memory Over-Provisioning Detection
"""

import argparse
import sys
from datetime import datetime, timedelta
from typing import Dict, List, Optional, Tuple
import json

try:
    import requests
    REQUESTS_AVAILABLE = True
except ImportError:
    REQUESTS_AVAILABLE = False
    print("Warning: requests library not available. Install with: pip install requests")

try:
    import numpy as np
    NUMPY_AVAILABLE = True
except ImportError:
    NUMPY_AVAILABLE = False
    print("Warning: numpy library not available. Install with: pip install numpy")


class CapacityAnalyzer:
    """Detect over-provisioning and recommend right-sizing"""

    # Cost estimates (USD per GB per month)
    COST_PER_GB_MONTH = 10.50

    # Thresholds
    OVER_PROVISIONED_THRESHOLD = 0.60  # < 60% p95 utilization
    HEADROOM_FACTOR = 1.20  # 20% headroom for recommendations

    def __init__(self, prometheus_url: str = "http://localhost:9090"):
        """
        Initialize the capacity analyzer

        Args:
            prometheus_url: URL of Prometheus server
        """
        self.prometheus_url = prometheus_url.rstrip('/')

    def _query_prometheus(self, query: str, start_time: datetime, end_time: datetime) -> List[Dict]:
        """
        Query Prometheus for metrics data

        Args:
            query: PromQL query string
            start_time: Start of time range
            end_time: End of time range

        Returns:
            List of metric data points
        """
        if not REQUESTS_AVAILABLE:
            raise RuntimeError("requests library is required. Install with: pip install requests")

        url = f"{self.prometheus_url}/api/v1/query_range"
        params = {
            'query': query,
            'start': start_time.timestamp(),
            'end': end_time.timestamp(),
            'step': '1h'  # 1 hour resolution
        }

        try:
            response = requests.get(url, params=params, timeout=30)
            response.raise_for_status()
            data = response.json()

            if data['status'] != 'success':
                raise RuntimeError(f"Prometheus query failed: {data}")

            return data['data']['result']
        except requests.exceptions.RequestException as e:
            raise RuntimeError(f"Failed to query Prometheus: {e}")

    def _calculate_percentiles(self, values: List[float]) -> Dict[str, float]:
        """
        Calculate memory usage percentiles

        Args:
            values: List of memory usage values

        Returns:
            Dictionary with avg, p50, p95, p99, max
        """
        if not values:
            return {'avg': 0, 'p50': 0, 'p95': 0, 'p99': 0, 'max': 0}

        if NUMPY_AVAILABLE:
            arr = np.array(values)
            return {
                'avg': float(np.mean(arr)),
                'p50': float(np.percentile(arr, 50)),
                'p95': float(np.percentile(arr, 95)),
                'p99': float(np.percentile(arr, 99)),
                'max': float(np.max(arr))
            }
        else:
            # Fallback to sorted list approach
            sorted_vals = sorted(values)
            n = len(sorted_vals)
            return {
                'avg': sum(values) / n,
                'p50': sorted_vals[int(n * 0.50)],
                'p95': sorted_vals[int(n * 0.95)],
                'p99': sorted_vals[int(n * 0.99)],
                'max': sorted_vals[-1]
            }

    def fetch_metrics(self, node_id: str, days: int = 7) -> Dict:
        """
        Fetch memory metrics for a specific node

        Args:
            node_id: Node identifier (e.g., "querynode-1")
            days: Number of days to analyze

        Returns:
            Dictionary with memory metrics
        """
        end_time = datetime.now()
        start_time = end_time - timedelta(days=days)

        # Query for memory usage (in bytes)
        memory_query = f'milvus_component_memory_bytes{{component="total",node_id="{node_id}"}}'

        try:
            results = self._query_prometheus(memory_query, start_time, end_time)

            if not results:
                raise ValueError(f"No metrics found for node {node_id}")

            # Extract values (convert bytes to GB)
            values_gb = []
            for result in results:
                for value in result['values']:
                    timestamp, val = value
                    values_gb.append(float(val) / (1024**3))  # Convert to GB

            if not values_gb:
                raise ValueError(f"No data points found for node {node_id}")

            # Get allocated memory from node configuration
            # In production, this would come from Kubernetes/node specs
            # For now, we estimate it as max usage + buffer
            allocated_gb = self._get_allocated_memory(node_id)

            stats = self._calculate_percentiles(values_gb)

            return {
                'allocated_memory_gb': allocated_gb,
                'avg_memory_gb': stats['avg'],
                'p50_memory_gb': stats['p50'],
                'p95_memory_gb': stats['p95'],
                'p99_memory_gb': stats['p99'],
                'max_memory_gb': stats['max'],
                'data_points': len(values_gb),
                'time_range_days': days
            }

        except Exception as e:
            raise RuntimeError(f"Failed to fetch metrics for {node_id}: {e}")

    def _get_allocated_memory(self, node_id: str) -> float:
        """
        Get allocated memory for a node from configuration

        In production, this would query Kubernetes or cloud provider APIs.
        For demo purposes, we use a mock based on common sizes.

        Args:
            node_id: Node identifier

        Returns:
            Allocated memory in GB
        """
        # Mock data for demonstration
        # In production, query: kubectl get pod <pod_name> -o json | jq .spec.containers[0].resources.limits.memory
        mock_allocations = {
            'querynode-1': 64.0,
            'querynode-2': 64.0,
            'querynode-3': 128.0,
            'querynode-4': 32.0,
            'querynode-5': 96.0,
        }

        return mock_allocations.get(node_id, 64.0)  # Default to 64GB

    def analyze_node(self, node_id: str, days: int = 7) -> Dict:
        """
        Analyze a single node for over-provisioning

        Args:
            node_id: Node identifier
            days: Number of days to analyze

        Returns:
            Analysis results with recommendations
        """
        try:
            metrics = self.fetch_metrics(node_id, days)
        except Exception as e:
            return {
                'node_id': node_id,
                'status': 'error',
                'error': str(e)
            }

        allocated = metrics['allocated_memory_gb']
        p95_usage = metrics['p95_memory_gb']
        p99_usage = metrics['p99_memory_gb']
        avg_usage = metrics['avg_memory_gb']
        max_usage = metrics['max_memory_gb']

        # Calculate utilization based on p95
        utilization = p95_usage / allocated if allocated > 0 else 0

        # Detect over-provisioning (< 60% p95 utilization)
        if utilization < self.OVER_PROVISIONED_THRESHOLD:
            # Recommend size: p99 usage + 20% headroom
            recommended = p99_usage * self.HEADROOM_FACTOR

            # Round up to common memory sizes (8, 16, 32, 64, 128, 256 GB)
            recommended = self._round_to_common_size(recommended)

            savings = allocated - recommended
            savings_pct = (savings / allocated) * 100 if allocated > 0 else 0
            savings_monthly = savings * self.COST_PER_GB_MONTH

            # Determine confidence level
            confidence = 'high' if days >= 7 else 'medium'
            if days >= 30:
                confidence = 'very_high'

            return {
                'node_id': node_id,
                'status': 'over_provisioned',
                'allocated_gb': allocated,
                'avg_usage_gb': avg_usage,
                'p95_usage_gb': p95_usage,
                'p99_usage_gb': p99_usage,
                'max_usage_gb': max_usage,
                'utilization_pct': utilization * 100,
                'recommended_gb': recommended,
                'savings_gb': savings,
                'savings_pct': savings_pct,
                'savings_monthly_usd': savings_monthly,
                'confidence': confidence,
                'data_points': metrics['data_points'],
                'time_range_days': days
            }

        # Node is optimally sized
        return {
            'node_id': node_id,
            'status': 'optimal',
            'allocated_gb': allocated,
            'avg_usage_gb': avg_usage,
            'p95_usage_gb': p95_usage,
            'p99_usage_gb': p99_usage,
            'utilization_pct': utilization * 100,
            'confidence': 'high' if days >= 7 else 'medium'
        }

    def _round_to_common_size(self, size_gb: float) -> float:
        """
        Round memory size to common allocation sizes

        Args:
            size_gb: Memory size in GB

        Returns:
            Rounded size
        """
        common_sizes = [8, 16, 24, 32, 48, 64, 96, 128, 192, 256, 384, 512]

        # Find the next larger common size
        for common_size in common_sizes:
            if common_size >= size_gb:
                return float(common_size)

        # If larger than all common sizes, round to nearest 64GB
        return float(((int(size_gb) + 63) // 64) * 64)

    def analyze_cluster(self, node_ids: List[str], days: int = 7) -> Dict:
        """
        Analyze entire cluster for over-provisioning

        Args:
            node_ids: List of node identifiers
            days: Number of days to analyze

        Returns:
            Cluster-wide analysis results
        """
        results = []
        over_provisioned = []
        optimal = []
        errors = []

        for node_id in node_ids:
            result = self.analyze_node(node_id, days)
            results.append(result)

            if result['status'] == 'over_provisioned':
                over_provisioned.append(result)
            elif result['status'] == 'optimal':
                optimal.append(result)
            else:
                errors.append(result)

        # Calculate totals
        total_savings_gb = sum(r['savings_gb'] for r in over_provisioned)
        total_savings_monthly = sum(r['savings_monthly_usd'] for r in over_provisioned)
        total_allocated = sum(r.get('allocated_gb', 0) for r in results if 'allocated_gb' in r)
        total_savings_pct = (total_savings_gb / total_allocated * 100) if total_allocated > 0 else 0

        return {
            'summary': {
                'total_nodes': len(node_ids),
                'over_provisioned_nodes': len(over_provisioned),
                'optimal_nodes': len(optimal),
                'error_nodes': len(errors),
                'total_allocated_gb': total_allocated,
                'total_savings_gb': total_savings_gb,
                'total_savings_pct': total_savings_pct,
                'total_savings_monthly_usd': total_savings_monthly,
                'analysis_days': days
            },
            'over_provisioned_nodes': over_provisioned,
            'optimal_nodes': optimal,
            'errors': errors,
            'all_results': results
        }


def format_cluster_report(analysis: Dict) -> str:
    """
    Format cluster analysis results as a human-readable report

    Args:
        analysis: Cluster analysis results

    Returns:
        Formatted report string
    """
    summary = analysis['summary']
    over_provisioned = analysis['over_provisioned_nodes']
    optimal = analysis['optimal_nodes']

    lines = []
    lines.append("=" * 80)
    lines.append("MILVUS MEMORY CAPACITY ANALYSIS REPORT")
    lines.append("=" * 80)
    lines.append("")
    lines.append(f"Analysis Period: {summary['analysis_days']} days")
    lines.append(f"Report Generated: {datetime.now().strftime('%Y-%m-%d %H:%M:%S')}")
    lines.append("")

    lines.append("SUMMARY")
    lines.append("-" * 80)
    lines.append(f"Total Nodes Analyzed:     {summary['total_nodes']}")
    lines.append(f"Over-Provisioned Nodes:   {summary['over_provisioned_nodes']}")
    lines.append(f"Optimally Sized Nodes:    {summary['optimal_nodes']}")
    lines.append(f"Nodes with Errors:        {summary['error_nodes']}")
    lines.append("")
    lines.append(f"Total Allocated Memory:   {summary['total_allocated_gb']:.1f} GB")
    lines.append(f"Potential Memory Savings: {summary['total_savings_gb']:.1f} GB ({summary['total_savings_pct']:.1f}%)")
    lines.append(f"Monthly Cost Savings:     ${summary['total_savings_monthly_usd']:.2f}")
    lines.append("")

    if over_provisioned:
        lines.append("OVER-PROVISIONED NODES")
        lines.append("=" * 80)
        lines.append("")

        for idx, node in enumerate(over_provisioned, 1):
            lines.append(f"{idx}. {node['node_id']}")
            lines.append(f"   Allocated:       {node['allocated_gb']:.1f} GB")
            lines.append(f"   Avg Usage:       {node['avg_usage_gb']:.1f} GB")
            lines.append(f"   P95 Usage:       {node['p95_usage_gb']:.1f} GB ({node['utilization_pct']:.1f}% utilization)")
            lines.append(f"   P99 Usage:       {node['p99_usage_gb']:.1f} GB")
            lines.append(f"   Max Usage:       {node['max_usage_gb']:.1f} GB")
            lines.append(f"   Recommendation:  Resize to {node['recommended_gb']:.0f} GB")
            lines.append(f"   Savings:         {node['savings_gb']:.1f} GB (${node['savings_monthly_usd']:.2f}/month)")
            lines.append(f"   Confidence:      {node['confidence'].upper()}")
            lines.append("")

    if optimal:
        lines.append("OPTIMALLY SIZED NODES")
        lines.append("=" * 80)
        lines.append("")

        for node in optimal:
            lines.append(f"  - {node['node_id']}: {node['allocated_gb']:.1f} GB allocated, "
                        f"{node['utilization_pct']:.1f}% utilization (P95)")
        lines.append("")

    if summary['over_provisioned_nodes'] > 0:
        lines.append("RECOMMENDATIONS")
        lines.append("=" * 80)
        lines.append("")
        lines.append("1. Review over-provisioned nodes and plan downsizing during maintenance window")
        lines.append("2. Monitor memory usage for 1-2 weeks after changes")
        lines.append("3. Set up alerts for memory usage >80% to prevent OOM")
        lines.append("4. Re-run this analysis monthly to track optimization opportunities")
        lines.append("")
        lines.append(f"Total Estimated Savings: ${summary['total_savings_monthly_usd']:.2f}/month "
                    f"(${summary['total_savings_monthly_usd'] * 12:.2f}/year)")
    else:
        lines.append("RECOMMENDATIONS")
        lines.append("=" * 80)
        lines.append("")
        lines.append("All nodes are optimally sized. No action needed at this time.")
        lines.append("Continue monitoring and re-analyze monthly for optimization opportunities.")

    lines.append("")
    lines.append("=" * 80)

    return "\n".join(lines)


def main():
    """Main CLI entry point"""
    parser = argparse.ArgumentParser(
        description='Milvus Memory Over-Provisioning Detection Tool',
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
Examples:
  # Analyze a single node
  %(prog)s --node querynode-1 --days 7

  # Analyze entire cluster
  %(prog)s --analyze-cluster --days 30

  # Analyze with custom Prometheus URL
  %(prog)s --analyze-cluster --prometheus http://prometheus:9090

  # Output as JSON
  %(prog)s --analyze-cluster --format json
        """
    )

    parser.add_argument(
        '--node',
        type=str,
        help='Analyze a specific node (e.g., querynode-1)'
    )

    parser.add_argument(
        '--analyze-cluster',
        action='store_true',
        help='Analyze all QueryNodes in the cluster'
    )

    parser.add_argument(
        '--nodes',
        type=str,
        nargs='+',
        help='List of node IDs to analyze (space-separated)'
    )

    parser.add_argument(
        '--days',
        type=int,
        default=7,
        help='Number of days to analyze (default: 7, recommended: 30 for high confidence)'
    )

    parser.add_argument(
        '--prometheus',
        type=str,
        default='http://localhost:9090',
        help='Prometheus server URL (default: http://localhost:9090)'
    )

    parser.add_argument(
        '--format',
        choices=['text', 'json'],
        default='text',
        help='Output format (default: text)'
    )

    parser.add_argument(
        '--threshold',
        type=float,
        default=0.60,
        help='Over-provisioning threshold for p95 utilization (default: 0.60 = 60%%)'
    )

    args = parser.parse_args()

    # Validate arguments
    if not args.node and not args.analyze_cluster and not args.nodes:
        parser.error("Must specify --node, --nodes, or --analyze-cluster")

    # Create analyzer
    analyzer = CapacityAnalyzer(prometheus_url=args.prometheus)
    analyzer.OVER_PROVISIONED_THRESHOLD = args.threshold

    try:
        if args.node:
            # Analyze single node
            result = analyzer.analyze_node(args.node, args.days)

            if args.format == 'json':
                print(json.dumps(result, indent=2))
            else:
                print(f"\nAnalyzing node: {args.node} ({args.days} days)\n")
                print("=" * 60)

                if result['status'] == 'error':
                    print(f"ERROR: {result['error']}")
                    sys.exit(1)
                elif result['status'] == 'over_provisioned':
                    print(f"Status: OVER-PROVISIONED")
                    print(f"Allocated:       {result['allocated_gb']:.1f} GB")
                    print(f"P95 Usage:       {result['p95_usage_gb']:.1f} GB ({result['utilization_pct']:.1f}% utilization)")
                    print(f"P99 Usage:       {result['p99_usage_gb']:.1f} GB")
                    print(f"Recommendation:  Resize to {result['recommended_gb']:.0f} GB")
                    print(f"Savings:         {result['savings_gb']:.1f} GB (${result['savings_monthly_usd']:.2f}/month)")
                    print(f"Confidence:      {result['confidence'].upper()}")
                else:
                    print(f"Status: OPTIMAL")
                    print(f"Allocated:       {result['allocated_gb']:.1f} GB")
                    print(f"P95 Usage:       {result['p95_usage_gb']:.1f} GB ({result['utilization_pct']:.1f}% utilization)")
                    print(f"No action needed.")

                print("=" * 60)

        else:
            # Analyze cluster or multiple nodes
            if args.analyze_cluster:
                # In production, discover nodes from Kubernetes/Prometheus
                # For demo, use a predefined list
                node_ids = [f'querynode-{i}' for i in range(1, 6)]
            else:
                node_ids = args.nodes

            print(f"\nAnalyzing {len(node_ids)} nodes over {args.days} days...\n")

            analysis = analyzer.analyze_cluster(node_ids, args.days)

            if args.format == 'json':
                print(json.dumps(analysis, indent=2))
            else:
                report = format_cluster_report(analysis)
                print(report)

    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)


if __name__ == '__main__':
    main()
