from __future__ import annotations

import argparse
import json
import statistics
import sys
import time
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
AI_ENGINE = ROOT / "apps" / "ai-engine"
sys.path.insert(0, str(AI_ENGINE))

from app.classification.service import ClassificationService  # noqa: E402


def sample_pages() -> list[dict]:
    texts = [
        "ABC Industrial Supplies",
        "Statement Period 01/11/2026 - 30/11/2026",
        "Previous Balance 12,450.00",
        "Payments Received 5,000.00",
        "Current Charges 2,130.00",
        "Current Balance 9,580.00",
        "Account Number 001-9921",
    ]
    return [{
        "page": 1,
        "ocr_blocks": [
            {
                "block_id": f"b{index:04d}",
                "text": text,
                "bbox": [0, 0, 10, 10],
                "confidence": 0.99,
            }
            for index, text in enumerate(texts, start=1)
        ],
    }]


def percentile(values: list[float], q: float) -> float:
    ordered = sorted(values)
    if not ordered:
        return 0.0
    index = min(len(ordered) - 1, max(0, int(round((len(ordered) - 1) * q))))
    return ordered[index]


def main() -> None:
    parser = argparse.ArgumentParser(description="Benchmark local document classification latency")
    parser.add_argument("--backend", choices=("rules", "modernbert"), default="rules")
    parser.add_argument("--model-dir", default="models/modernbert")
    parser.add_argument("--iterations", type=int, default=25)
    parser.add_argument("--warmup", type=int, default=3)
    parser.add_argument("--max-length", type=int, default=256)
    parser.add_argument("--provider", choices=("cpu", "cuda", "auto"), default="cpu")
    args = parser.parse_args()

    service = ClassificationService(
        backend=args.backend,
        model_dir=args.model_dir,
        model_provider=args.provider,
        model_max_length=args.max_length,
    )
    health = service.health()
    if not health.get("ready"):
        raise SystemExit("classifier is not ready: " + json.dumps(health))

    pages = sample_pages()
    for _ in range(max(0, args.warmup)):
        service.classify(document_id="warmup", pages=pages)

    timings: list[float] = []
    last_result: dict = {}
    for index in range(max(1, args.iterations)):
        started = time.perf_counter()
        last_result = service.classify(document_id=f"bench-{index}", pages=pages)
        timings.append((time.perf_counter() - started) * 1000.0)

    result = {
        "backend": args.backend,
        "provider": health.get("provider", "rules"),
        "iterations": len(timings),
        "document_type": last_result.get("document_type"),
        "confidence": last_result.get("confidence"),
        "p50_ms": round(statistics.median(timings), 3),
        "p95_ms": round(percentile(timings, 0.95), 3),
        "min_ms": round(min(timings), 3),
        "max_ms": round(max(timings), 3),
    }
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
