#!/usr/bin/env python3
import argparse
import json
import sys
import time
from typing import Iterable


def load_lines(path: str) -> Iterable[str]:
    with open(path, "r", encoding="utf-8") as handle:
        for raw in handle:
            line = raw.strip()
            if not line:
                continue
            yield line


def main() -> None:
    parser = argparse.ArgumentParser(description="Replay JSON fixtures to stdout.")
    parser.add_argument("--input", required=True, help="Path to fixture file (json lines)")
    parser.add_argument("--count", type=int, default=1, help="Repeat count")
    parser.add_argument("--interval-ms", type=int, default=100, help="Delay between lines")
    args = parser.parse_args()

    lines = list(load_lines(args.input))
    if not lines:
        print("no lines found", file=sys.stderr)
        sys.exit(1)

    for _ in range(args.count):
        for line in lines:
            try:
                json.loads(line)
            except json.JSONDecodeError as exc:
                print(f"invalid json: {exc}", file=sys.stderr)
                sys.exit(1)
            print(line, flush=True)
            time.sleep(args.interval_ms / 1000.0)


if __name__ == "__main__":
    main()
