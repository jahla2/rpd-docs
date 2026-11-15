from __future__ import annotations

import argparse
import json
from pathlib import Path


def normalize(value) -> str:
    if value is None:
        return ""
    return " ".join(str(value).strip().split()).casefold()


def field_accuracy(documents: list[dict]) -> tuple[int, int, float]:
    correct = 0
    total = 0
    for document in documents:
        expected = document.get("expected_fields") or {}
        actual = document.get("actual_fields") or {}
        for field, expected_value in expected.items():
            total += 1
            if normalize(actual.get(field)) == normalize(expected_value):
                correct += 1
    return correct, total, correct / total if total else 1.0


def line_cell_accuracy(documents: list[dict]) -> tuple[int, int, float]:
    correct = 0
    total = 0
    for document in documents:
        expected_rows = document.get("expected_line_items") or []
        actual_rows = document.get("actual_line_items") or []
        for row_index, expected_row in enumerate(expected_rows):
            actual_row = actual_rows[row_index] if row_index < len(actual_rows) else {}
            for field, expected_value in expected_row.items():
                total += 1
                if normalize(actual_row.get(field)) == normalize(expected_value):
                    correct += 1
    return correct, total, correct / total if total else 1.0


def reconciliation_metrics(payload: dict) -> tuple[float, float, float]:
    expected = {tuple(item) for item in payload.get("expected_matches") or []}
    actual = {tuple(item) for item in payload.get("actual_matches") or []}
    true_positive = len(expected & actual)
    precision = true_positive / len(actual) if actual else (1.0 if not expected else 0.0)
    recall = true_positive / len(expected) if expected else 1.0
    f1 = 0.0 if precision + recall == 0 else 2 * precision * recall / (precision + recall)
    return precision, recall, f1


def evaluate(path: Path) -> dict:
    payload = json.loads(path.read_text(encoding="utf-8"))
    field_correct, field_total, fields = field_accuracy(payload.get("documents") or [])
    line_correct, line_total, lines = line_cell_accuracy(payload.get("documents") or [])
    precision, recall, f1 = reconciliation_metrics(payload.get("reconciliation") or {})
    return {
        "field_accuracy": fields,
        "field_correct": field_correct,
        "field_total": field_total,
        "line_cell_accuracy": lines,
        "line_cell_correct": line_correct,
        "line_cell_total": line_total,
        "reconciliation_precision": precision,
        "reconciliation_recall": recall,
        "reconciliation_f1": f1,
    }


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("fixture", type=Path)
    parser.add_argument("--min-field-accuracy", type=float, default=0.95)
    parser.add_argument("--min-line-accuracy", type=float, default=0.95)
    parser.add_argument("--min-reconciliation-f1", type=float, default=0.95)
    args = parser.parse_args()

    metrics = evaluate(args.fixture)
    print(json.dumps(metrics, indent=2, sort_keys=True))

    failures = []
    if metrics["field_accuracy"] < args.min_field_accuracy:
        failures.append("field_accuracy")
    if metrics["line_cell_accuracy"] < args.min_line_accuracy:
        failures.append("line_cell_accuracy")
    if metrics["reconciliation_f1"] < args.min_reconciliation_f1:
        failures.append("reconciliation_f1")
    if failures:
        print("Threshold failures:", ", ".join(failures))
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
