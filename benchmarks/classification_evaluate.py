from __future__ import annotations

import argparse
import json
import statistics
import sys
from collections import Counter
from pathlib import Path
from typing import Any


ROOT = Path(__file__).resolve().parents[1]
AI_ENGINE = ROOT / "apps" / "ai-engine"
sys.path.insert(0, str(AI_ENGINE))

from app.classification.rules import DOCUMENT_TYPES  # noqa: E402
from app.classification.service import ClassificationService  # noqa: E402


def load_corpus(path: Path) -> list[dict[str, Any]]:
    if path.suffix.lower() == ".jsonl":
        documents: list[dict[str, Any]] = []
        for line_number, raw in enumerate(path.read_text(encoding="utf-8").splitlines(), start=1):
            line = raw.strip()
            if not line or line.startswith("#"):
                continue
            try:
                item = json.loads(line)
            except json.JSONDecodeError as exc:
                raise ValueError(f"{path}:{line_number}: invalid JSON: {exc}") from exc
            if not isinstance(item, dict):
                raise ValueError(f"{path}:{line_number}: each JSONL row must be an object")
            documents.append(item)
    else:
        payload = json.loads(path.read_text(encoding="utf-8"))
        documents = payload.get("documents") if isinstance(payload, dict) else payload
        if not isinstance(documents, list):
            raise ValueError("classification corpus must be a JSON array or an object with documents[]")

    if not documents:
        raise ValueError("classification corpus is empty")

    for index, item in enumerate(documents, start=1):
        expected = str(item.get("expected_type", "")).strip()
        if expected not in DOCUMENT_TYPES:
            raise ValueError(
                f"document {index} has unsupported expected_type {expected!r}; "
                f"expected one of {', '.join(DOCUMENT_TYPES)}"
            )
        if not item.get("pages") and not item.get("text_pages") and not str(item.get("text", "")).strip():
            raise ValueError(f"document {index} must provide pages, text_pages, or text")
    return documents


def pages_for(item: dict[str, Any]) -> list[dict[str, Any]]:
    pages = item.get("pages")
    if isinstance(pages, list) and pages:
        return pages

    text_pages = item.get("text_pages")
    if not isinstance(text_pages, list) or not text_pages:
        text_pages = [str(item.get("text", ""))]

    normalized_pages: list[dict[str, Any]] = []
    for page_index, page_text in enumerate(text_pages, start=1):
        lines = [line.strip() for line in str(page_text).splitlines() if line.strip()]
        if not lines and str(page_text).strip():
            lines = [str(page_text).strip()]
        normalized_pages.append(
            {
                "page": page_index,
                "ocr_blocks": [
                    {
                        "block_id": f"p{page_index:03d}-b{block_index:04d}",
                        "text": text,
                        "bbox": [0, 0, 10, 10],
                        "confidence": 0.99,
                    }
                    for block_index, text in enumerate(lines, start=1)
                ],
            }
        )
    return normalized_pages


def percentile(values: list[float], q: float) -> float:
    ordered = sorted(values)
    if not ordered:
        return 0.0
    index = min(len(ordered) - 1, max(0, int(round((len(ordered) - 1) * q))))
    return ordered[index]


def _prf(expected: list[str], predicted: list[str]) -> dict[str, Any]:
    expected_labels = sorted(set(expected))
    labels = sorted(set(expected) | set(predicted))
    per_class: dict[str, dict[str, float | int]] = {}

    for label in labels:
        tp = sum(1 for e, p in zip(expected, predicted) if e == label and p == label)
        fp = sum(1 for e, p in zip(expected, predicted) if e != label and p == label)
        fn = sum(1 for e, p in zip(expected, predicted) if e == label and p != label)
        support = sum(1 for e in expected if e == label)
        precision = tp / (tp + fp) if tp + fp else 0.0
        recall = tp / (tp + fn) if tp + fn else 0.0
        f1 = 0.0 if precision + recall == 0 else (2 * precision * recall) / (precision + recall)
        per_class[label] = {
            "support": support,
            "true_positive": tp,
            "false_positive": fp,
            "false_negative": fn,
            "precision": round(precision, 6),
            "recall": round(recall, 6),
            "f1": round(f1, 6),
        }

    macro_precision = statistics.mean(float(per_class[label]["precision"]) for label in expected_labels)
    macro_recall = statistics.mean(float(per_class[label]["recall"]) for label in expected_labels)
    macro_f1 = statistics.mean(float(per_class[label]["f1"]) for label in expected_labels)
    accuracy = sum(1 for e, p in zip(expected, predicted) if e == p) / len(expected)

    return {
        "accuracy": round(accuracy, 6),
        "macro_precision": round(macro_precision, 6),
        "macro_recall": round(macro_recall, 6),
        "macro_f1": round(macro_f1, 6),
        "per_class": per_class,
    }


def _rollup(label: str) -> str:
    if label == "invoice":
        return "invoice"
    if label == "purchase_order":
        return "purchase_order"
    return "other"


def evaluate(
    service: ClassificationService,
    documents: list[dict[str, Any]],
) -> dict[str, Any]:
    expected: list[str] = []
    predicted: list[str] = []
    review_flags: list[bool] = []
    methods: Counter[str] = Counter()
    latencies: list[float] = []
    fallback_errors = 0
    records: list[dict[str, Any]] = []

    for index, item in enumerate(documents, start=1):
        document_id = str(item.get("id") or f"corpus-{index:04d}")
        expected_type = str(item["expected_type"]).strip()
        result = service.classify(
            document_id=document_id,
            pages=pages_for(item),
            document_type_hint=item.get("document_type_hint"),
            hint_source=item.get("hint_source"),
        )
        predicted_type = str(result.get("document_type", "unknown"))
        requires_review = bool(result.get("requires_review", predicted_type == "unknown"))
        expected.append(expected_type)
        predicted.append(predicted_type)
        review_flags.append(requires_review)
        methods[str(result.get("method", "unknown"))] += 1
        latencies.append(float(result.get("latency_ms", 0.0)))
        if result.get("fallback_error"):
            fallback_errors += 1
        records.append(
            {
                "id": document_id,
                "expected_type": expected_type,
                "predicted_type": predicted_type,
                "confidence": result.get("confidence"),
                "method": result.get("method"),
                "requires_review": requires_review,
                "latency_ms": result.get("latency_ms"),
                "fallback_error": result.get("fallback_error"),
            }
        )

    full = _prf(expected, predicted)
    rolled = _prf([_rollup(value) for value in expected], [_rollup(value) for value in predicted])

    reviewed = sum(review_flags)
    covered_indices = [i for i, review in enumerate(review_flags) if not review]
    covered_correct = sum(1 for i in covered_indices if expected[i] == predicted[i])
    covered_accuracy = covered_correct / len(covered_indices) if covered_indices else 1.0

    confusion: dict[str, dict[str, int]] = {}
    for expected_type, predicted_type in zip(expected, predicted):
        row = confusion.setdefault(expected_type, {})
        row[predicted_type] = row.get(predicted_type, 0) + 1

    return {
        "documents": len(documents),
        "correct": sum(1 for e, p in zip(expected, predicted) if e == p),
        **full,
        "invoice_purchase_order_other": rolled,
        "review_required": reviewed,
        "review_rate": round(reviewed / len(documents), 6),
        "coverage": round((len(documents) - reviewed) / len(documents), 6),
        "covered_accuracy": round(covered_accuracy, 6),
        "fallback_errors": fallback_errors,
        "method_counts": dict(sorted(methods.items())),
        "latency_ms": {
            "mean": round(statistics.mean(latencies), 3) if latencies else 0.0,
            "p50": round(statistics.median(latencies), 3) if latencies else 0.0,
            "p95": round(percentile(latencies, 0.95), 3),
            "max": round(max(latencies), 3) if latencies else 0.0,
        },
        "confusion_matrix": confusion,
        "records": records,
    }


def main() -> int:
    parser = argparse.ArgumentParser(description="Evaluate document-classification accuracy and review routing")
    parser.add_argument("corpus", type=Path)
    parser.add_argument("--backend", choices=("rules", "modernbert"), default="rules")
    parser.add_argument("--model-dir", default="models/modernbert")
    parser.add_argument("--model-file", default="onnx/model_int8.onnx")
    parser.add_argument("--provider", choices=("cpu", "cuda", "auto"), default="cpu")
    parser.add_argument("--max-length", type=int, default=256)
    parser.add_argument("--rule-threshold", type=float, default=0.90)
    parser.add_argument("--model-threshold", type=float, default=0.72)
    parser.add_argument("--model-margin", type=float, default=0.08)
    parser.add_argument("--min-accuracy", type=float, default=0.0)
    parser.add_argument("--min-macro-f1", type=float, default=0.0)
    parser.add_argument("--min-covered-accuracy", type=float, default=0.0)
    parser.add_argument("--max-review-rate", type=float, default=1.0)
    parser.add_argument("--fail-on-fallback-error", action="store_true")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()

    service = ClassificationService(
        backend=args.backend,
        model_dir=str(args.model_dir),
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

    metrics = evaluate(service, load_corpus(args.corpus))
    report = {
        "backend": args.backend,
        "provider": health.get("provider", "rules"),
        "model": health.get("model"),
        "model_version": health.get("model_version"),
        "thresholds": {
            "rule_accept_confidence": args.rule_threshold,
            "model_accept_confidence": args.model_threshold,
            "model_min_margin": args.model_margin,
        },
        **metrics,
    }

    rendered = json.dumps(report, indent=2, sort_keys=True)
    print(rendered)
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(rendered + "\n", encoding="utf-8")

    failures: list[str] = []
    if metrics["accuracy"] < args.min_accuracy:
        failures.append(f"accuracy<{args.min_accuracy}")
    if metrics["macro_f1"] < args.min_macro_f1:
        failures.append(f"macro_f1<{args.min_macro_f1}")
    if metrics["covered_accuracy"] < args.min_covered_accuracy:
        failures.append(f"covered_accuracy<{args.min_covered_accuracy}")
    if metrics["review_rate"] > args.max_review_rate:
        failures.append(f"review_rate>{args.max_review_rate}")
    if args.fail_on_fallback_error and metrics["fallback_errors"]:
        failures.append("fallback_errors>0")

    if failures:
        print("Threshold failures:", ", ".join(failures), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
