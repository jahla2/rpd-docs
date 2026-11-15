from __future__ import annotations

import argparse
import json
import statistics
import time
from pathlib import Path

import httpx


def percentile(values: list[float], p: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    index = min(len(ordered) - 1, max(0, round((len(ordered) - 1) * p)))
    return ordered[index]


def main() -> int:
    parser = argparse.ArgumentParser(description="Benchmark the deployed document API.")
    parser.add_argument("--base-url", required=True)
    parser.add_argument("--input-dir", type=Path, required=True)
    parser.add_argument("--api-key", default="")
    parser.add_argument("--timeout-seconds", type=float, default=180.0)
    args = parser.parse_args()

    files = sorted(
        path for path in args.input_dir.iterdir()
        if path.suffix.lower() in {".pdf", ".png", ".jpg", ".jpeg", ".tif", ".tiff"}
    )
    if not files:
        raise SystemExit("no benchmark documents found")

    headers = {}
    if args.api_key:
        headers["Authorization"] = f"Bearer {args.api_key}"

    latencies: list[float] = []
    successes = 0

    with httpx.Client(base_url=args.base_url.rstrip("/"), headers=headers, timeout=30.0) as client:
        for path in files:
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
            while time.monotonic() < deadline:
                job = client.get(f"/api/v1/jobs/{job_id}")
                job.raise_for_status()
                status = job.json()["status"]
                if status in {"READY_FOR_RECONCILIATION", "REVIEW_REQUIRED"}:
                    successes += 1
                    latencies.append(time.perf_counter() - started)
                    break
                if status == "FAILED":
                    break
                time.sleep(0.25)

    summary = {
        "documents": len(files),
        "successes": successes,
        "success_rate": successes / len(files),
        "latency_seconds": {
            "mean": statistics.mean(latencies) if latencies else 0.0,
            "p50": percentile(latencies, 0.50),
            "p95": percentile(latencies, 0.95),
            "max": max(latencies) if latencies else 0.0,
        },
    }
    print(json.dumps(summary, indent=2))
    return 0 if successes == len(files) else 1


if __name__ == "__main__":
    raise SystemExit(main())
