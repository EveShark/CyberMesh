from __future__ import annotations

import json
import urllib.error
import urllib.request
import base64
from dataclasses import dataclass
from typing import Optional


@dataclass(frozen=True)
class RegistryConfig:
    enabled: bool
    url: str
    subject_flow: str
    subject_feature: str
    timeout_sec: int
    allow_fallback: bool
    username: str = ""
    password: str = ""


class RegistryClient:
    def __init__(self, cfg: RegistryConfig):
        self.cfg = cfg

    def enabled(self) -> bool:
        return bool(self.cfg.enabled)

    def subject_latest_id(self, subject: str) -> int:
        if not self.enabled():
            raise ValueError("registry disabled")
        if not subject:
            raise ValueError("subject required")
        url = f"{self.cfg.url.rstrip('/')}/subjects/{subject}/versions/latest"
        data = self._get(url)
        return int(data.get("id", 0))

    def schema_by_id(self, schema_id: int) -> str:
        if not self.enabled():
            raise ValueError("registry disabled")
        if schema_id <= 0:
            raise ValueError("schema id required")
        url = f"{self.cfg.url.rstrip('/')}/schemas/ids/{schema_id}"
        data = self._get(url)
        return str(data.get("schema", ""))

    def _get(self, url: str) -> dict:
        req = urllib.request.Request(url)
        req.add_header("Accept", "application/json")
        self._apply_auth(req)
        try:
            with urllib.request.urlopen(req, timeout=self.cfg.timeout_sec) as resp:
                body = resp.read().decode("utf-8")
                return json.loads(body)
        except urllib.error.HTTPError as exc:
            raise ValueError(f"registry status {exc.code}") from exc
        except urllib.error.URLError as exc:
            raise ValueError(f"registry error {exc}") from exc

    def _apply_auth(self, req: urllib.request.Request) -> None:
        if not self.cfg.username and not self.cfg.password:
            return
        token = f"{self.cfg.username}:{self.cfg.password}".encode("utf-8")
        encoded = base64.b64encode(token).decode("ascii")
        req.add_header("Authorization", f"Basic {encoded}")
