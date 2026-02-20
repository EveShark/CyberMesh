from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
import json
from typing import Any, Dict, List


@dataclass(frozen=True)
class Schema:
    name: str
    required: List[str]
    properties: Dict[str, Any]


def _load_schema_file(path: Path) -> Schema:
    data = json.loads(path.read_text(encoding="utf-8"))
    return Schema(
        name=data.get("properties", {}).get("schema", {}).get("const", ""),
        required=data.get("required", []),
        properties=data.get("properties", {}),
    )


def load_flow_schema() -> Schema:
    base_dir = Path(__file__).resolve().parents[1]
    return _load_schema_file(base_dir / "schemas" / "flow.v1.json")


def load_cic_schema() -> Schema:
    base_dir = Path(__file__).resolve().parents[1]
    return _load_schema_file(base_dir / "schemas" / "cic.v1.json")
