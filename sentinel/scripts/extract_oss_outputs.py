"""Extract OSS output examples from upstream repos into a local folder."""

from __future__ import annotations

import argparse
import json
import re
import shutil
import subprocess
import tempfile
import time
import zipfile
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any, Dict, Iterable, List, Optional, Tuple
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen, urlretrieve


REPOS = [
    {"name": "mcp-scan", "url": "https://github.com/invariantlabs-ai/mcp-scan"},
    {"name": "skill-scanner", "url": "https://github.com/cisco-ai-defense/skill-scanner"},
    {"name": "trufflehog", "url": "https://github.com/trufflesecurity/trufflehog"},
    {"name": "rebuff", "url": "https://github.com/protectai/rebuff"},
    {"name": "sigma", "url": "https://github.com/SigmaHQ/sigma"},
    {"name": "bzar", "url": "https://github.com/mitre-attack/bzar"},
    {"name": "falco", "url": "https://github.com/falcosecurity/falco"},
    {"name": "zeek", "url": "https://github.com/zeek/zeek"},
]

CANDIDATE_DIRS = {
    "examples",
    "example",
    "demo",
    "demos",
    "sample",
    "samples",
    "testdata",
    "tests",
    "fixtures",
    "docs",
    "doc",
    "data",
    "outputs",
    "output",
    "report",
    "reports",
}

TEXT_EXTS = {".log", ".txt", ".out", ".yaml", ".yml"}
JSON_EXTS = {".json", ".ndjson"}
MD_EXTS = {".md", ".markdown"}

MAX_FILE_BYTES_DEFAULT = 512 * 1024
MAX_JSON_OBJECTS_DEFAULT = 2000


@dataclass
class OutputArtifact:
    source: str
    output_path: str
    kind: str
    size_bytes: int
    records: int


@dataclass
class RepoResult:
    name: str
    url: str
    default_branch: str
    outputs: List[OutputArtifact] = field(default_factory=list)
    warnings: List[str] = field(default_factory=list)


def _repo_slug(repo_url: str) -> Tuple[str, str]:
    parts = repo_url.rstrip("/").split("/")
    if len(parts) < 2:
        raise ValueError(f"Invalid repo url: {repo_url}")
    return parts[-2], parts[-1]


def _http_json(url: str) -> Dict[str, Any]:
    req = Request(
        url,
        headers={
            "Accept": "application/vnd.github+json",
            "User-Agent": "sentinel-oss-output-extractor",
        },
    )
    with urlopen(req, timeout=30) as resp:
        data = resp.read().decode("utf-8")
    return json.loads(data)


def _get_default_branch(repo_url: str) -> str:
    owner, repo = _repo_slug(repo_url)
    api_url = f"https://api.github.com/repos/{owner}/{repo}"
    try:
        data = _http_json(api_url)
    except Exception:
        return "main"
    return str(data.get("default_branch", "main"))


def _git_available() -> bool:
    return shutil.which("git") is not None


def _clone_repo(repo_url: str, dest: Path, branch: str) -> None:
    if _git_available():
        subprocess.check_call(
            ["git", "clone", "--depth", "1", "--branch", branch, repo_url, str(dest)],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.DEVNULL,
        )
        return
    _download_zip(repo_url, dest, branch)


def _download_zip(repo_url: str, dest: Path, branch: str) -> None:
    owner, repo = _repo_slug(repo_url)
    zip_url = f"https://github.com/{owner}/{repo}/archive/refs/heads/{branch}.zip"
    archive_path = dest.parent / f"{repo}-{branch}.zip"
    try:
        urlretrieve(zip_url, archive_path)
    except (HTTPError, URLError):
        if branch != "master":
            _download_zip(repo_url, dest, "master")
            return
        raise
    with zipfile.ZipFile(archive_path, "r") as zf:
        zf.extractall(dest.parent)
    archive_path.unlink(missing_ok=True)
    extracted = next(dest.parent.glob(f"{repo}-*"), None)
    if not extracted:
        raise RuntimeError(f"Failed to extract archive for {repo_url}")
    extracted.rename(dest)


def _iter_candidate_files(repo_root: Path) -> Iterable[Path]:
    for child in repo_root.iterdir():
        if child.is_dir() and child.name.lower() in CANDIDATE_DIRS:
            yield from child.rglob("*")

    for name in ("README.md", "readme.md", "README.markdown"):
        path = repo_root / name
        if path.exists():
            yield path


def _safe_name(value: str) -> str:
    return re.sub(r"[^A-Za-z0-9_.-]+", "_", value.strip())[:180]


def _write_output(
    output_dir: Path,
    base_name: str,
    content: str,
    kind: str,
) -> Path:
    suffix = ".ndjson" if kind == "ndjson" else ".json" if kind == "json" else ".txt"
    filename = _safe_name(base_name) + suffix
    path = output_dir / filename
    path.write_text(content, encoding="utf-8")
    return path


def _extract_json_records(text: str) -> Tuple[List[Any], bool]:
    text = text.strip()
    if not text:
        return [], False
    if text.startswith("{") or text.startswith("["):
        try:
            data = json.loads(text)
            if isinstance(data, list):
                return data, True
            return [data], True
        except Exception:
            return [], False
    return [], False


def _extract_ndjson_records(text: str, max_objects: int) -> List[Any]:
    records: List[Any] = []
    for line in text.splitlines():
        if not line.strip():
            continue
        try:
            record = json.loads(line)
        except Exception:
            continue
        records.append(record)
        if len(records) >= max_objects:
            break
    return records


def _extract_code_blocks(text: str) -> List[Tuple[str, str]]:
    blocks: List[Tuple[str, str]] = []
    pattern = re.compile(r"```([a-zA-Z0-9_-]*)\n(.*?)\n```", re.DOTALL)
    for match in pattern.finditer(text):
        lang = (match.group(1) or "").strip().lower()
        body = match.group(2).strip()
        blocks.append((lang, body))
    return blocks


def _scan_file_for_outputs(
    path: Path,
    output_dir: Path,
    max_bytes: int,
    max_objects: int,
) -> List[OutputArtifact]:
    artifacts: List[OutputArtifact] = []
    if not path.is_file():
        return artifacts
    if path.stat().st_size > max_bytes:
        return artifacts

    text = path.read_text(encoding="utf-8", errors="ignore")
    rel_name = str(path)

    if path.suffix.lower() in JSON_EXTS:
        records, ok = _extract_json_records(text)
        if ok:
            out_path = _write_output(output_dir, f"file_{rel_name}", text, "json")
            artifacts.append(OutputArtifact(
                source=f"file:{rel_name}",
                output_path=str(out_path),
                kind="json",
                size_bytes=out_path.stat().st_size,
                records=len(records),
            ))
            return artifacts
        ndjson = _extract_ndjson_records(text, max_objects)
        if ndjson:
            out_path = _write_output(output_dir, f"file_{rel_name}", "\n".join(json.dumps(r) for r in ndjson), "ndjson")
            artifacts.append(OutputArtifact(
                source=f"file:{rel_name}",
                output_path=str(out_path),
                kind="ndjson",
                size_bytes=out_path.stat().st_size,
                records=len(ndjson),
            ))
        return artifacts

    if path.suffix.lower() in TEXT_EXTS:
        ndjson = _extract_ndjson_records(text, max_objects)
        if ndjson:
            out_path = _write_output(output_dir, f"file_{rel_name}", "\n".join(json.dumps(r) for r in ndjson), "ndjson")
            artifacts.append(OutputArtifact(
                source=f"file:{rel_name}",
                output_path=str(out_path),
                kind="ndjson",
                size_bytes=out_path.stat().st_size,
                records=len(ndjson),
            ))
        return artifacts

    if path.suffix.lower() in MD_EXTS:
        for idx, (lang, body) in enumerate(_extract_code_blocks(text), start=1):
            if not body:
                continue
            if lang in ("json", "ndjson") or body.startswith("{") or body.startswith("["):
                records, ok = _extract_json_records(body)
                if ok:
                    out_path = _write_output(output_dir, f"codeblock_{rel_name}_{idx}", body, "json")
                    artifacts.append(OutputArtifact(
                        source=f"codeblock:{rel_name}#{idx}",
                        output_path=str(out_path),
                        kind="json",
                        size_bytes=out_path.stat().st_size,
                        records=len(records),
                    ))
                else:
                    ndjson = _extract_ndjson_records(body, max_objects)
                    if ndjson:
                        out_path = _write_output(
                            output_dir,
                            f"codeblock_{rel_name}_{idx}",
                            "\n".join(json.dumps(r) for r in ndjson),
                            "ndjson",
                        )
                        artifacts.append(OutputArtifact(
                            source=f"codeblock:{rel_name}#{idx}",
                            output_path=str(out_path),
                            kind="ndjson",
                            size_bytes=out_path.stat().st_size,
                            records=len(ndjson),
                        ))
        return artifacts

    return artifacts


def _extract_repo_outputs(
    repo_root: Path,
    output_root: Path,
    max_bytes: int,
    max_objects: int,
) -> List[OutputArtifact]:
    output_root.mkdir(parents=True, exist_ok=True)
    artifacts: List[OutputArtifact] = []
    for path in _iter_candidate_files(repo_root):
        artifacts.extend(_scan_file_for_outputs(path, output_root, max_bytes, max_objects))
    return artifacts


def _write_repo_summary(output_root: Path, result: RepoResult) -> None:
    summary_path = output_root / "summary.json"
    summary = {
        "name": result.name,
        "url": result.url,
        "default_branch": result.default_branch,
        "outputs": [
            {
                "source": o.source,
                "output_path": o.output_path,
                "kind": o.kind,
                "size_bytes": o.size_bytes,
                "records": o.records,
            }
            for o in result.outputs
        ],
        "warnings": result.warnings,
    }
    summary_path.write_text(json.dumps(summary, indent=2), encoding="utf-8")


def _write_index(output_dir: Path, results: List[RepoResult]) -> None:
    index = {
        "generated_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "repos": [
            {
                "name": r.name,
                "url": r.url,
                "default_branch": r.default_branch,
                "outputs": len(r.outputs),
                "warnings": r.warnings,
            }
            for r in results
        ],
    }
    (output_dir / "index.json").write_text(json.dumps(index, indent=2), encoding="utf-8")


def _write_review(output_dir: Path, results: List[RepoResult]) -> None:
    lines = []
    lines.append("# OSS Output Review")
    lines.append("")
    lines.append(f"Generated: {time.strftime('%Y-%m-%d %H:%M:%S UTC', time.gmtime())}")
    lines.append("")
    lines.append("| Repo | Outputs | Notes |")
    lines.append("| --- | --- | --- |")
    for result in results:
        note = "; ".join(result.warnings) if result.warnings else "ok"
        lines.append(f"| {result.name} | {len(result.outputs)} | {note} |")
    lines.append("")
    for result in results:
        lines.append(f"## {result.name}")
        if not result.outputs:
            lines.append("- No outputs detected by heuristics.")
        else:
            for output in result.outputs[:5]:
                lines.append(f"- {output.kind} from {output.source} -> {output.output_path}")
        lines.append("")
    (output_dir / "OUTPUTS-REVIEW.md").write_text("\n".join(lines), encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser(description="Extract OSS outputs from upstream repos.")
    parser.add_argument(
        "--output-dir",
        default=str(Path(__file__).resolve().parents[1] / "oss_outputs"),
        help="Directory to write extracted outputs",
    )
    parser.add_argument(
        "--max-file-bytes",
        type=int,
        default=MAX_FILE_BYTES_DEFAULT,
        help="Max file size to inspect",
    )
    parser.add_argument(
        "--max-json-objects",
        type=int,
        default=MAX_JSON_OBJECTS_DEFAULT,
        help="Max JSON objects to capture from a file",
    )
    args = parser.parse_args()

    output_dir = Path(args.output_dir)
    output_dir.mkdir(parents=True, exist_ok=True)

    results: List[RepoResult] = []

    with tempfile.TemporaryDirectory() as tmpdir:
        tmp_root = Path(tmpdir)
        for repo in REPOS:
            name = repo["name"]
            url = repo["url"]
            branch = _get_default_branch(url)
            result = RepoResult(name=name, url=url, default_branch=branch)
            repo_dir = tmp_root / name
            try:
                _clone_repo(url, repo_dir, branch)
            except Exception as exc:
                result.warnings.append(f"clone_failed: {exc}")
                results.append(result)
                continue

            repo_output = output_dir / name
            outputs = _extract_repo_outputs(
                repo_dir,
                repo_output,
                max_bytes=args.max_file_bytes,
                max_objects=args.max_json_objects,
            )
            result.outputs = outputs
            if not outputs:
                result.warnings.append("no_outputs_detected")
            _write_repo_summary(repo_output, result)
            results.append(result)

    _write_index(output_dir, results)
    _write_review(output_dir, results)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
