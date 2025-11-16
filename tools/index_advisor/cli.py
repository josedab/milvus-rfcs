#!/usr/bin/env python3
"""Interactive CLI for Index Advisor"""

from advisor import IndexAdvisor, UseCase
import questionary
from rich.console import Console
from rich.table import Table
from rich import box


def main():
    console = Console()

    console.print("\n[bold cyan]🚀 Milvus Index Advisor[/bold cyan]\n")

    # Gather requirements interactively
    num_vectors = questionary.text(
        "How many vectors will you store?",
        validate=lambda x: x.isdigit() and int(x) > 0,
    ).ask()
    num_vectors = int(num_vectors)

    dimensions = questionary.text(
        "Vector dimensions?",
        validate=lambda x: x.isdigit() and int(x) > 0,
    ).ask()
    dimensions = int(dimensions)

    latency = questionary.select(
        "Latency requirement?",
        choices=[
            "< 10ms (real-time)",
            "< 30ms (interactive)",
            "< 50ms (responsive)",
            "< 100ms (acceptable)",
            "> 100ms (batch)",
        ],
    ).ask()
    latency_ms = {"< 10ms (real-time)": 10, "< 30ms (interactive)": 30, "< 50ms (responsive)": 50, "< 100ms (acceptable)": 100, "> 100ms (batch)": 200}[latency]

    memory = questionary.text(
        "Memory budget per QueryNode (GB)?",
        default="32",
        validate=lambda x: x.replace(".", "").isdigit(),
    ).ask()
    memory_gb = float(memory)

    qps = questionary.text(
        "Expected QPS?",
        default="1000",
        validate=lambda x: x.isdigit(),
    ).ask()
    qps_target = int(qps)

    use_case = questionary.select(
        "Primary use case?",
        choices=[uc.value for uc in UseCase],
    ).ask()

    has_gpu = questionary.confirm("Do you have GPUs available?").ask()

    # Generate recommendation
    console.print("\n[bold]Analyzing requirements...[/bold] ✓\n")

    advisor = IndexAdvisor()
    rec = advisor.recommend(
        num_vectors=num_vectors,
        dimensions=dimensions,
        latency_requirement_ms=latency_ms,
        memory_budget_gb=memory_gb,
        qps_target=qps_target,
        use_case=use_case,
        has_gpu=has_gpu,
    )

    # Display recommendation
    console.print("=" * 70)
    console.print(f"  [bold green]RECOMMENDED INDEX: {rec.index_type.value}[/bold green]")
    console.print("=" * 70)
    console.print(f"\n[bold]Reason:[/bold] {rec.reason}")
    console.print(f"[bold]Confidence:[/bold] {rec.confidence * 100:.0f}%\n")

    # Parameters table
    params_table = Table(title="Parameters", box=box.ROUNDED)
    params_table.add_column("Parameter", style="cyan")
    params_table.add_column("Value", style="green")

    for key, value in rec.params.items():
        params_table.add_row(key, str(value))

    console.print(params_table)

    # Performance estimates
    perf_table = Table(title="Expected Performance", box=box.ROUNDED)
    perf_table.add_column("Metric", style="cyan")
    perf_table.add_column("Value", style="yellow")
    perf_table.add_column("Status", style="green")

    perf_table.add_row(
        "Memory",
        f"{rec.memory_gb:.1f} GB",
        "✓" if rec.memory_gb < memory_gb else "⚠️ Exceeds budget"
    )
    perf_table.add_row("Build Time", f"~{rec.build_time_min:.0f} minutes", "")
    perf_table.add_row(
        "Query Latency (p95)",
        f"~{rec.query_latency_p95:.0f} ms",
        "✓" if rec.query_latency_p95 < latency_ms * 1.2 else "⚠️ May exceed target"
    )
    perf_table.add_row("Recall@10", f"~{rec.recall_at_10 * 100:.0f}%", "")

    console.print(perf_table)

    # Alternatives
    if rec.alternatives:
        console.print("\n[bold]Alternatives to Consider:[/bold]")
        for alt in rec.alternatives[:2]:
            console.print(f"  • [cyan]{alt.index_type.value}[/cyan]: "
                         f"{alt.memory_gb:.1f} GB memory, "
                         f"~{alt.query_latency_p95:.0f}ms latency")

    console.print("\n")


if __name__ == "__main__":
    main()
