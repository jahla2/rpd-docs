from __future__ import annotations

import argparse
import json
import statistics
import sys
import time
from collections import Counter
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
AI_ENGINE = ROOT / "apps" / "ai-engine"
sys.path.insert(0, str(AI_ENGINE))

from app.classification.service import ClassificationService  # noqa: E402
from classification_evaluate import load_corpus, pages_for  # noqa: E402


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
    parser.add_argument("--model-file", default="onnx/model_int8.onnx")
    parser.add_argument("--corpus", type=Path)
    parser.add_argument("--iterations", type=int, default=25)
    parser.add_argument("--warmup", type=int, default=3)
    parser.add_argument("--max-length", type=int, default=256)
    parser.add_argument("--provider", choices=("cpu", "cuda", "auto"), default="cpu")
    parser.add_argument("--rule-threshold", type=float, default=0.90)
    parser.add_argument("--model-threshold", type=float, default=0.72)
    parser.add_argument("--model-margin", type=float, default=0.08)
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()

    service = ClassificationService(
        backend=args.backend,
        model_dir=args.model_dir,
        model_file=args.model_file,
        model_provider=args.provider,
        model_max_length=args.max_length,
        rule_accept_confidence=args.rule_threshold,
        model_accept_confidence=args.model_threshold,
        model_min_margin=args.model_margin,
    )
    health = service.health()
    if not health.get("ready"):
        raise SystemExit("classifier is not ready: " + json.dumps(health))

    if args.corpus:
        items = load_corpus(args.corpus)
        documents = [
            (str(item.get("id") or f"corpus-{index:04d}"), pages_for(item))
            for index, item in enumerate(items, start=1)
        ]
    else:
        documents = [("sample", sample_pages())]

    warmup_calls = max(0, args.warmup)
    for index in range(warmup_calls):
        document_id, pages = documents[index % len(documents)]
        service.classify(document_id=f"warmup-{document_id}-{index}", pages=pages)

    timings: list[float] = []
    methods: Counter[str] = Counter()
    document_types: Counter[str] = Counter()
    last_result: dict = {}
    for index in range(max(1, args.iterations)):
        document_id, pages = documents[index % len(documents)]
        started = time.perf_counter()
        last_result = service.classify(document_id=f"bench-{document_id}-{index}", pages=pages)
        timings.append((time.perf_counter() - started) * 1000.0)
        methods[str(last_result.get("method", "unknown"))] += 1
        document_types[str(last_result.get("document_type", "unknown"))] += 1

    result = {
        "backend": args.backend,
        "provider": health.get("provider", "rules"),
        "model": health.get("model"),
        "model_version": health.get("model_version"),
        "samples": len(timings),
        "corpus_documents": len(documents),
        "method_counts": dict(sorted(methods.items())),
        "document_type_counts": dict(sorted(document_types.items())),
        "p50_ms": round(statistics.median(timings), 3),
        "p95_ms": round(percentile(timings, 0.95), 3),
        "mean_ms": round(statistics.mean(timings), 3),
        "min_ms": round(min(timings), 3),
        "max_ms": round(max(timings), 3),
    }
    rendered = json.dumps(result, indent=2, sort_keys=True)
    print(rendered)
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(rendered + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
