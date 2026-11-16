from __future__ import annotations

import argparse
import json
import statistics
import time
from collections import defaultdict
from datetime import datetime
from pathlib import Path
from typing import Any

import httpx


STAGE_ORDER = (
    "PREPROCESS",
    "OCR",
    "CLASSIFY",
    "TABLE",
    "MAPPING",
    "VALIDATION",
    "FINALIZE",
)
TERMINAL_SUCCESS = {"READY_FOR_RECONCILIATION", "REVIEW_REQUIRED", "COMPLETED"}


def percentile(values: list[float], p: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    index = min(len(ordered) - 1, max(0, round((len(ordered) - 1) * p)))
    return ordered[index]


def latency_summary(values: list[float]) -> dict[str, float | int]:
    return {
        "count": len(values),
        "mean": round(statistics.mean(values), 6) if values else 0.0,
        "p50": round(statistics.median(values), 6) if values else 0.0,
        "p95": round(percentile(values, 0.95), 6),
        "max": round(max(values), 6) if values else 0.0,
    }


def parse_timestamp(value: Any) -> datetime | None:
    if not value:
        return None
    text = str(value).strip()
    if text.endswith("Z"):
        text = text[:-1] + "+00:00"
    try:
        return datetime.fromisoformat(text)
    except ValueError:
        return None


def stage_durations(items: list[dict[str, Any]]) -> dict[str, float]:
    durations: dict[str, float] = {}
    for item in items:
        started = parse_timestamp(item.get("started_at"))
        completed = parse_timestamp(item.get("completed_at"))
        if started is None or completed is None:
            continue
        duration = max(0.0, (completed - started).total_seconds())
        durations[str(item.get("step_name", "UNKNOWN"))] = duration
    return durations


def main() -> int:
    parser = argparse.ArgumentParser(description="Benchmark the deployed document API and pipeline stages.")
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--input-dir", type=Path, required=True)
    parser.add_argument("--api-key", default="")
    parser.add_argument("--timeout-seconds", type=float, default=180.0)
    parser.add_argument("--repeat", type=int, default=1)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()

    files = sorted(
        path for path in args.input_dir.iterdir()
        if path.suffix.lower() in {".pdf", ".png", ".jpg", ".jpeg", ".tif", ".tiff"}
    )
    if not files:
        raise SystemExit("no benchmark documents found")
    if args.repeat < 1:
        raise SystemExit("--repeat must be at least 1")

    headers = {}
    if args.api_key:
        headers["Authorization"] = f"Bearer {args.api_key}"

    work_items = [(path, iteration) for iteration in range(1, args.repeat + 1) for path in files]
    total_latencies: list[float] = []
    stage_samples: dict[str, list[float]] = defaultdict(list)
    successes = 0
    details: list[dict[str, Any]] = []

    with httpx.Client(base_url=args.base_url.rstrip("/"), headers=headers, timeout=30.0) as client:
        for path, iteration in work_items:
            started = time.perf_counter()
            with path.open("rb") as handle:
                response = client.post(
                    "/api/v1/documents",
                    files={"file": (path.name, handle, "application/octet-stream")},
                )
            response.raise_for_status()
            body = response.json()
            job_id = body["job_id"]

            deadline = time.monotonic() + args.timeout_seconds
            terminal_status = "TIMEOUT"
            stages: dict[str, float] = {}
            while time.monotonic() < deadline:
                job = client.get(f"/api/v1/jobs/{job_id}")
                job.raise_for_status()
                terminal_status = str(job.json()["status"])
                if terminal_status in TERMINAL_SUCCESS | {"FAILED", "CANCELLED"}:
                    break
                time.sleep(0.25)

            total_seconds = time.perf_counter() - started
            if terminal_status in TERMINAL_SUCCESS:
                successes += 1
                total_latencies.append(total_seconds)
                steps = client.get(f"/api/v1/jobs/{job_id}/steps")
                steps.raise_for_status()
                stages = stage_durations(list(steps.json().get("items") or []))
                for stage_name, duration in stages.items():
                    stage_samples[stage_name].append(duration)

            details.append(
                {
                    "file": path.name,
                    "iteration": iteration,
                    "job_id": job_id,
                    "status": terminal_status,
                    "total_seconds": round(total_seconds, 6),
                    "stage_seconds": {key: round(value, 6) for key, value in stages.items()},
                }
            )

    ordered_stage_names = list(STAGE_ORDER)
    ordered_stage_names.extend(sorted(name for name in stage_samples if name not in STAGE_ORDER))
    summary = {
        "documents": len(work_items),
        "unique_documents": len(files),
        "repeat": args.repeat,
        "successes": successes,
        "success_rate": round(successes / len(work_items), 6),
        "latency_seconds": latency_summary(total_latencies),
        "stage_latency_seconds": {
            stage_name: latency_summary(stage_samples.get(stage_name, []))
            for stage_name in ordered_stage_names
            if stage_samples.get(stage_name)
        },
        "documents_detail": details,
    }

    rendered = json.dumps(summary, indent=2, sort_keys=False)
    print(rendered)
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(rendered + "\n", encoding="utf-8")
    return 0 if successes == len(work_items) else 1


if __name__ == "__main__":
    raise SystemExit(main())
