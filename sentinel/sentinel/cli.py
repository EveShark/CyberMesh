"""Sentinel CLI interface."""

import sys
import time
from pathlib import Path

if __package__ in (None, ""):
    package_root = Path(__file__).resolve().parent.parent
    if str(package_root) not in sys.path:
        sys.path.insert(0, str(package_root))
    __package__ = "sentinel"
from typing import Optional, List

import typer
from rich.console import Console
from rich.table import Table
from rich.progress import Progress, SpinnerColumn, TextColumn
from rich.panel import Panel

from .config import load_settings, Settings
from .logging import configure_logging, get_logger
from .parsers import detect_file_type, get_parser_for_file, FileType
from .providers import ProviderRegistry, ThreatLevel
from .providers.static import EntropyProvider, StringsProvider
from .router import SmartRouter, RoutingConfig, RoutingStrategy
from .output import ThreatReport, VisualReportGenerator
from .utils.id_gen import generate_analysis_id

app = typer.Typer(
    name="sentinel",
    help="Sentinel - Agentic File Scanner with Multi-Model Detection",
    add_completion=False,
)

console = Console()
logger = get_logger(__name__)


def get_settings() -> Settings:
    """Load settings with error handling."""
    try:
        return load_settings()
    except Exception as e:
        console.print(f"[red]Failed to load settings: {e}[/red]")
        console.print("Make sure .env file exists or environment variables are set.")
        raise typer.Exit(1)


def setup_registry(settings: Settings) -> ProviderRegistry:
    """Set up provider registry with available providers."""
    registry_path = Path("data/provider_registry.json")
    registry = ProviderRegistry(str(registry_path))
    
    registry.register(EntropyProvider(), baseline_weight=0.6)
    registry.register(StringsProvider(), baseline_weight=0.7)
    
    if settings.models.models_path.exists():
        try:
            from .providers.ml import MalwarePEProvider
            ml_provider = MalwarePEProvider(
                models_path=str(settings.models.models_path),
                threshold=settings.detection.malware_threshold,
            )
            registry.register(ml_provider, baseline_weight=0.85)
        except Exception as e:
            logger.warning(f"Could not load ML provider: {e}")
    
    return registry


@app.command()
def analyze(
    file_path: str = typer.Argument(..., help="Path to file to analyze"),
    output: str = typer.Option("console", "--output", "-o", help="Output format: console, json, html"),
    providers: Optional[str] = typer.Option(None, "--providers", "-p", help="Comma-separated provider names"),
    verbose: bool = typer.Option(False, "--verbose", "-v", help="Verbose output"),
    report_dir: Optional[str] = typer.Option(None, "--report-dir", help="Directory for HTML reports"),
):
    """Analyze a file for threats."""
    settings = get_settings()
    configure_logging(
        level="DEBUG" if verbose else settings.log_level,
        json_format=settings.log_format == "json",
    )
    
    path = Path(file_path)
    if not path.exists():
        console.print(f"[red]File not found: {file_path}[/red]")
        raise typer.Exit(1)
    
    if not path.is_file():
        console.print(f"[red]Not a file: {file_path}[/red]")
        raise typer.Exit(1)
    
    registry = setup_registry(settings)
    router = SmartRouter(registry, RoutingConfig(strategy=RoutingStrategy.ENSEMBLE))
    
    t0 = time.perf_counter()
    
    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        console=console,
        transient=True,
    ) as progress:
        task = progress.add_task("Detecting file type...", total=None)
        file_type = detect_file_type(file_path)
        
        progress.update(task, description="Parsing file...")
        parser = get_parser_for_file(file_path)
        
        if parser is None:
            console.print(f"[yellow]No parser available for file type: {file_type.value}[/yellow]")
            console.print("Supported types: PE, PDF, Office, Script, Android, PCAP")
            raise typer.Exit(1)
        
        parsed_file = parser.parse(file_path)
        
        progress.update(task, description="Selecting providers...")
        if providers:
            provider_names = [p.strip() for p in providers.split(",")]
        else:
            provider_names = router.route(file_type)
        
        if not provider_names:
            console.print("[yellow]No providers available for this file type[/yellow]")
            raise typer.Exit(1)
        
        results = []
        for name in provider_names:
            provider = registry.get_provider(name)
            if provider and provider.can_analyze(parsed_file):
                progress.update(task, description=f"Running {name}...")
                result = provider.analyze(parsed_file)
                results.append(result)
                registry.update_latency(name, result.latency_ms)
    
    analysis_time = (time.perf_counter() - t0) * 1000
    
    report = ThreatReport.from_analysis_results(
        file_id=generate_analysis_id(parsed_file.hashes.get("sha256")),
        file_hash=parsed_file.hashes.get("sha256", "unknown"),
        file_name=parsed_file.file_name,
        file_type=parsed_file.file_type.value,
        file_size=parsed_file.file_size,
        results=results,
        analysis_time_ms=analysis_time,
    )
    
    if output == "json":
        console.print(report.to_json())
    
    elif output == "html":
        output_dir = report_dir or str(settings.output.report_output_dir)
        generator = VisualReportGenerator(output_dir)
        report_path = generator.save(report)
        console.print(f"[green]Report saved to: {report_path}[/green]")
    
    else:
        _print_console_report(report, verbose)
    
    if report.threat_level in (ThreatLevel.MALICIOUS, ThreatLevel.CRITICAL):
        raise typer.Exit(2)
    elif report.threat_level == ThreatLevel.SUSPICIOUS:
        raise typer.Exit(1)


def _print_console_report(report: ThreatReport, verbose: bool = False):
    """Print report to console."""
    threat_colors = {
        ThreatLevel.CLEAN: "green",
        ThreatLevel.SUSPICIOUS: "yellow",
        ThreatLevel.MALICIOUS: "red",
        ThreatLevel.CRITICAL: "red bold",
        ThreatLevel.UNKNOWN: "dim",
    }
    
    color = threat_colors.get(report.threat_level, "white")
    
    console.print()
    console.print(Panel(
        f"[bold]{report.file_name}[/bold]\n"
        f"Type: {report.file_type} | Size: {report.file_size:,} bytes\n"
        f"SHA256: {report.file_hash[:32]}...",
        title="SENTINEL THREAT REPORT",
        border_style="blue",
    ))
    
    console.print()
    console.print(f"  Threat Level: [{color}]{report.threat_level.value.upper()}[/{color}]")
    console.print(f"  Score: {report.final_score:.0%} | Confidence: {report.confidence:.0%}")
    console.print(f"  Analysis Time: {report.analysis_time_ms:.0f}ms")
    
    console.print()
    table = Table(title="Provider Verdicts", show_header=True, header_style="bold")
    table.add_column("Provider", style="cyan")
    table.add_column("Verdict", justify="center")
    table.add_column("Score", justify="right")
    table.add_column("Confidence", justify="right")
    
    for v in report.provider_verdicts:
        v_color = threat_colors.get(v.threat_level, "white")
        table.add_row(
            v.provider_name,
            f"[{v_color}]{v.threat_level.value.upper()}[/{v_color}]",
            f"{v.score:.0%}",
            f"{v.confidence:.0%}",
        )
    
    console.print(table)
    
    if report.findings and verbose:
        console.print()
        console.print("[bold]Findings:[/bold]")
        for finding in report.findings[:10]:
            console.print(f"  - {finding}")
    
    if report.indicators and verbose:
        console.print()
        console.print("[bold]Indicators:[/bold]")
        for ind in report.indicators[:10]:
            console.print(f"  [{ind['type']}] {ind['value']}")


@app.command()
def providers(
    action: str = typer.Argument("list", help="Action: list, stats"),
):
    """Manage detection providers."""
    settings = get_settings()
    configure_logging(level=settings.log_level)
    
    registry = setup_registry(settings)
    
    if action == "list":
        table = Table(title="Registered Providers", show_header=True)
        table.add_column("Name", style="cyan")
        table.add_column("Version")
        table.add_column("Weight", justify="right")
        table.add_column("Accuracy", justify="right")
        table.add_column("Calls", justify="right")
        table.add_column("Enabled", justify="center")
        
        for name, stats in registry.stats.items():
            table.add_row(
                name,
                stats.version,
                f"{stats.weight:.3f}",
                f"{stats.accuracy:.1%}" if stats.total_calls > 0 else "-",
                str(stats.total_calls),
                "[green]Yes[/green]" if stats.enabled else "[red]No[/red]",
            )
        
        console.print(table)
    
    elif action == "stats":
        summary = registry.get_summary()
        console.print(f"Total providers: {summary['total_providers']}")
        console.print(f"Enabled providers: {summary['enabled_providers']}")


@app.command()
def agentic(
    file_path: str = typer.Argument(..., help="Path to file to analyze"),
    output: str = typer.Option("console", "--output", "-o", help="Output format: console, json, html"),
    verbose: bool = typer.Option(False, "--verbose", "-v", help="Verbose output"),
    enable_llm: bool = typer.Option(True, "--llm/--no-llm", help="Enable LLM reasoning (Groq)"),
    report_dir: Optional[str] = typer.Option(None, "--report-dir", help="Directory for HTML reports"),
):
    """Analyze a file using the agentic workflow (LangGraph)."""
    settings = get_settings()
    configure_logging(
        level="DEBUG" if verbose else settings.log_level,
        json_format=settings.log_format == "json",
    )
    
    path = Path(file_path)
    if not path.exists():
        console.print(f"[red]File not found: {file_path}[/red]")
        raise typer.Exit(1)
    
    from .agents import AnalysisEngine
    
    with Progress(
        SpinnerColumn(),
        TextColumn("[progress.description]{task.description}"),
        console=console,
        transient=True,
    ) as progress:
        task = progress.add_task("Initializing agentic analysis...", total=None)
        
        engine = AnalysisEngine(
            models_path=str(settings.models.models_path),
            llm_api_key=settings.llm.glm_api_key if enable_llm else None,
            enable_llm=enable_llm,
        )
        
        progress.update(task, description="Running agentic analysis...")
        result = engine.analyze(str(path.absolute()))
    
    # Print results
    _print_agentic_report(result, verbose)
    
    if output == "json":
        import json
        output_data = {
            "file_path": result.get("file_path"),
            "threat_level": result.get("threat_level", ThreatLevel.UNKNOWN).value,
            "final_score": result.get("final_score", 0.0),
            "confidence": result.get("confidence", 0.0),
            "findings": [
                {"source": f.source, "severity": f.severity, "description": f.description}
                for f in result.get("findings", [])
            ],
            "reasoning_steps": result.get("reasoning_steps", []),
            "final_reasoning": result.get("final_reasoning", ""),
            "analysis_time_ms": result.get("analysis_time_ms", 0.0),
            "stages_completed": result.get("stages_completed", []),
        }
        console.print(json.dumps(output_data, indent=2))
    
    elif output == "html":
        # Convert to ThreatReport for HTML generation
        parsed_file = result.get("parsed_file")
        if parsed_file:
            from .output import ThreatReport, VisualReportGenerator, ProviderVerdict
            
            # Build verdicts from results
            verdicts = []
            for r in result.get("static_results", []) + result.get("ml_results", []):
                verdicts.append(ProviderVerdict(
                    provider_name=r.provider_name,
                    threat_level=r.threat_level,
                    score=r.score,
                    confidence=r.confidence,
                    findings=r.findings,
                ))
            
            report = ThreatReport(
                file_id=generate_analysis_id(parsed_file.hashes.get("sha256")),
                file_hash=parsed_file.hashes.get("sha256", "unknown"),
                file_name=parsed_file.file_name,
                file_type=parsed_file.file_type.value,
                file_size=parsed_file.file_size,
                threat_level=result.get("threat_level", ThreatLevel.UNKNOWN),
                final_score=result.get("final_score", 0.0),
                confidence=result.get("confidence", 0.0),
                provider_verdicts=verdicts,
                findings=[f.description for f in result.get("findings", [])],
                indicators=result.get("indicators", []),
                reasoning=result.get("final_reasoning", ""),
                analysis_time_ms=result.get("analysis_time_ms", 0.0),
            )
            
            output_dir = report_dir or str(settings.output.report_output_dir)
            generator = VisualReportGenerator(output_dir)
            report_path = generator.save(report)
            console.print(f"[green]Report saved to: {report_path}[/green]")
    
    # Exit code based on threat level
    threat_level = result.get("threat_level", ThreatLevel.UNKNOWN)
    if threat_level in (ThreatLevel.MALICIOUS, ThreatLevel.CRITICAL):
        raise typer.Exit(2)
    elif threat_level == ThreatLevel.SUSPICIOUS:
        raise typer.Exit(1)


def _print_agentic_report(result: dict, verbose: bool = False):
    """Print agentic analysis report."""
    threat_colors = {
        ThreatLevel.CLEAN: "green",
        ThreatLevel.SUSPICIOUS: "yellow",
        ThreatLevel.MALICIOUS: "red",
        ThreatLevel.CRITICAL: "red bold",
        ThreatLevel.UNKNOWN: "dim",
    }
    
    parsed_file = result.get("parsed_file")
    threat_level = result.get("threat_level", ThreatLevel.UNKNOWN)
    color = threat_colors.get(threat_level, "white")
    
    if parsed_file:
        console.print()
        console.print(Panel(
            f"[bold]{parsed_file.file_name}[/bold]\n"
            f"Type: {parsed_file.file_type.value} | Size: {parsed_file.file_size:,} bytes\n"
            f"SHA256: {parsed_file.hashes.get('sha256', 'unknown')[:32]}...",
            title="SENTINEL AGENTIC ANALYSIS",
            border_style="magenta",
        ))
    
    console.print()
    console.print(f"  Threat Level: [{color}]{threat_level.value.upper()}[/{color}]")
    console.print(f"  Score: {result.get('final_score', 0):.0%} | Confidence: {result.get('confidence', 0):.0%}")
    console.print(f"  Analysis Time: {result.get('analysis_time_ms', 0):.0f}ms")
    console.print(f"  Stages: {' -> '.join(result.get('stages_completed', []))}")
    
    # Findings table
    findings = result.get("findings", [])
    if findings:
        console.print()
        table = Table(title="Findings", show_header=True, header_style="bold")
        table.add_column("Source", style="cyan")
        table.add_column("Severity", justify="center")
        table.add_column("Description")
        
        severity_colors = {"low": "green", "medium": "yellow", "high": "red", "critical": "red bold"}
        
        for f in findings[:15]:
            sev_color = severity_colors.get(f.severity, "white")
            # Sanitize description for Windows console (replace problematic unicode)
            desc = f.description.encode('ascii', 'replace').decode('ascii')
            table.add_row(
                f.source,
                f"[{sev_color}]{f.severity.upper()}[/{sev_color}]",
                desc[:60] + "..." if len(desc) > 60 else desc,
            )
        
        console.print(table)
    
    # Reasoning chain
    if verbose:
        reasoning = result.get("reasoning_steps", [])
        if reasoning:
            console.print()
            console.print("[bold]Reasoning Chain:[/bold]")
            for step in reasoning:
                console.print(f"  -> {step}")
        
        final = result.get("final_reasoning", "")
        if final:
            console.print()
            console.print("[bold]Final Assessment:[/bold]")
            console.print(f"  {final}")


@app.command()
def benchmark(
    provider_name: Optional[str] = typer.Argument(None, help="Provider to benchmark (or 'all')"),
    verbose: bool = typer.Option(False, "--verbose", "-v", help="Show per-sample results"),
):
    """Benchmark providers against the evaluation suite."""
    settings = get_settings()
    configure_logging(level="DEBUG" if verbose else settings.log_level)
    
    from .eval import EvalSuite, ProviderBenchmark
    
    # Load eval suite
    eval_suite = EvalSuite()
    
    if eval_suite.sample_count == 0:
        console.print("[yellow]No evaluation samples found.[/yellow]")
        console.print("Add samples with: sentinel eval-add <file> <clean|malicious>")
        raise typer.Exit(1)
    
    console.print(f"[cyan]Eval suite: {eval_suite.sample_count} samples "
                  f"({eval_suite.malicious_count} malicious, {eval_suite.clean_count} clean)[/cyan]")
    
    # Setup registry and providers
    registry = setup_registry(settings)
    benchmarker = ProviderBenchmark(eval_suite)
    
    if provider_name and provider_name != "all":
        # Benchmark single provider
        provider = registry.get_provider(provider_name)
        if not provider:
            console.print(f"[red]Provider not found: {provider_name}[/red]")
            raise typer.Exit(1)
        
        providers_to_bench = [provider]
    else:
        # Benchmark all providers
        providers_to_bench = registry.get_all_providers()
    
    results = []
    for provider in providers_to_bench:
        with Progress(SpinnerColumn(), TextColumn("[progress.description]{task.description}"),
                      console=console, transient=True) as progress:
            progress.add_task(f"Benchmarking {provider.name}...", total=None)
            result = benchmarker.benchmark(provider)
            results.append(result)
            
            # Update registry with benchmark weight
            registry.set_baseline_weight(provider.name, result.baseline_weight)
    
    # Display results
    console.print()
    table = Table(title="Benchmark Results", show_header=True, header_style="bold")
    table.add_column("Provider", style="cyan")
    table.add_column("Accuracy", justify="right")
    table.add_column("Precision", justify="right")
    table.add_column("Recall", justify="right")
    table.add_column("F1 Score", justify="right")
    table.add_column("Baseline Weight", justify="right", style="green")
    table.add_column("Avg Latency", justify="right")
    
    for r in sorted(results, key=lambda x: x.f1_score, reverse=True):
        table.add_row(
            r.provider_name,
            f"{r.accuracy:.1%}",
            f"{r.precision:.1%}",
            f"{r.recall:.1%}",
            f"{r.f1_score:.3f}",
            f"{r.baseline_weight:.3f}",
            f"{r.avg_latency_ms:.1f}ms",
        )
    
    console.print(table)
    
    if verbose:
        for r in results:
            console.print(f"\n[bold]{r.provider_name} - Per-Sample Results:[/bold]")
            for sr in r.sample_results:
                status = "[green]CORRECT[/green]" if sr["correct"] else "[red]WRONG[/red]"
                console.print(f"  {sr['label']}: predicted={sr['predicted']}, actual={sr['actual']} {status}")


@app.command()
def eval_add(
    file_path: str = typer.Argument(..., help="Path to file"),
    verdict: str = typer.Argument(..., help="Ground truth: clean, suspicious, malicious"),
    label: str = typer.Option("", "--label", "-l", help="Label for the sample"),
):
    """Add a sample to the evaluation suite."""
    from .eval import EvalSuite
    
    path = Path(file_path)
    if not path.exists():
        console.print(f"[red]File not found: {file_path}[/red]")
        raise typer.Exit(1)
    
    verdict_map = {
        "clean": ThreatLevel.CLEAN,
        "suspicious": ThreatLevel.SUSPICIOUS,
        "malicious": ThreatLevel.MALICIOUS,
        "critical": ThreatLevel.CRITICAL,
    }
    
    if verdict.lower() not in verdict_map:
        console.print(f"[red]Invalid verdict: {verdict}[/red]")
        console.print("Valid options: clean, suspicious, malicious, critical")
        raise typer.Exit(1)
    
    eval_suite = EvalSuite()
    eval_suite.add_sample(
        file_path=str(path.absolute()),
        ground_truth=verdict_map[verdict.lower()],
        label=label or path.stem,
    )
    
    console.print(f"[green]Added sample: {path.name} ({verdict})[/green]")
    console.print(f"Total samples: {eval_suite.sample_count}")


@app.command()
def version():
    """Show version information."""
    from . import __version__
    console.print(f"Sentinel v{__version__}")


def main():
    """Main entry point."""
    app()


if __name__ == "__main__":
    main()
