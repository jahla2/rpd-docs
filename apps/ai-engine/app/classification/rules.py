from __future__ import annotations

import re
import time
from typing import Any


DOCUMENT_TYPES: tuple[str, ...] = (
    "invoice",
    "purchase_order",
    "credit_note",
    "debit_note",
    "receipt",
    "bank_statement",
    "supplier_statement",
    "payment_advice",
    "delivery_receipt",
    "goods_received_note",
    "erp_export",
    "spreadsheet",
    "unknown",
)

RULES: dict[str, tuple[tuple[str, float], ...]] = {
    "invoice": (
        ("tax invoice", 8.0),
        ("invoice number", 5.0),
        ("invoice no", 5.0),
        ("invoice date", 3.0),
        ("grand total", 3.0),
        ("amount due", 2.0),
        ("subtotal", 2.0),
        ("vat", 1.0),
    ),
    "purchase_order": (
        ("purchase order", 8.0),
        ("po number", 4.0),
        ("po no", 4.0),
        ("order date", 2.0),
        ("ship to", 2.0),
        ("ordered by", 2.0),
    ),
    "credit_note": (
        ("credit note", 10.0),
        ("credit memo", 10.0),
        ("credit amount", 3.0),
        ("amount credited", 3.0),
    ),
    "debit_note": (
        ("debit note", 10.0),
        ("debit memo", 10.0),
        ("debit amount", 3.0),
        ("amount debited", 3.0),
    ),
    "receipt": (
        ("official receipt", 8.0),
        ("cash receipt", 8.0),
        ("payment receipt", 7.0),
        ("amount tendered", 3.0),
        ("change due", 3.0),
        ("receipt no", 3.0),
    ),
    "bank_statement": (
        ("bank statement", 9.0),
        ("statement period", 4.0),
        ("opening balance", 3.0),
        ("closing balance", 3.0),
        ("account number", 2.0),
        ("debit credit", 2.0),
    ),
    "supplier_statement": (
        ("statement of account", 9.0),
        ("supplier statement", 9.0),
        ("previous balance", 3.0),
        ("balance brought forward", 3.0),
        ("current balance", 2.0),
    ),
    "payment_advice": (
        ("payment advice", 10.0),
        ("remittance advice", 10.0),
        ("payment reference", 3.0),
        ("beneficiary", 2.0),
        ("amount remitted", 3.0),
    ),
    "delivery_receipt": (
        ("delivery receipt", 10.0),
        ("delivery date", 3.0),
        ("delivered to", 3.0),
        ("received by", 2.0),
    ),
    "goods_received_note": (
        ("goods received note", 10.0),
        ("goods receipt note", 10.0),
        ("grn number", 4.0),
        ("received quantity", 3.0),
        ("goods received", 3.0),
    ),
    "erp_export": (
        ("erp export", 10.0),
        ("company code", 3.0),
        ("posting date", 3.0),
        ("document number", 2.0),
        ("fiscal year", 2.0),
    ),
    "spreadsheet": (
        ("spreadsheet", 10.0),
        ("worksheet", 6.0),
    ),
}

_MAX_PAGES = 4
_MAX_BLOCKS_PER_PAGE = 180
_MAX_TEXT_CHARS = 12000


def _normalize(value: str) -> str:
    value = value.lower()
    value = re.sub(r"[^a-z0-9]+", " ", value)
    return " ".join(value.split())


def build_document_text(pages: list[dict[str, Any]]) -> str:
    parts: list[str] = []
    size = 0
    for page in pages[:_MAX_PAGES]:
        for block in (page.get("ocr_blocks") or [])[:_MAX_BLOCKS_PER_PAGE]:
            text = str(block.get("text", "")).strip()
            if not text:
                continue
            normalized = _normalize(text)
            if not normalized:
                continue
            remaining = _MAX_TEXT_CHARS - size
            if remaining <= 0:
                return " ".join(parts)
            normalized = normalized[:remaining]
            parts.append(normalized)
            size += len(normalized) + 1
    return " ".join(parts)


def _rank(text: str) -> list[tuple[str, float]]:
    ranked: list[tuple[str, float]] = []
    for document_type, rules in RULES.items():
        score = sum(weight for phrase, weight in rules if phrase in text)
        ranked.append((document_type, score))
    ranked.sort(key=lambda item: (-item[1], item[0]))
    return ranked


def _alternatives(ranked: list[tuple[str, float]]) -> list[dict[str, Any]]:
    positives = [(name, score) for name, score in ranked if score > 0][:3]
    total = sum(score for _, score in positives) or 1.0
    return [{"type": name, "confidence": round(score / total, 5)} for name, score in positives]


def classify_document(
    *,
    document_id: str,
    pages: list[dict[str, Any]],
    document_type_hint: str | None = None,
    hint_source: str | None = None,
) -> dict[str, Any]:
    started = time.perf_counter_ns()
    hint = _normalize(document_type_hint or "").replace(" ", "_")
    if hint and hint in DOCUMENT_TYPES:
        latency_ms = max(0, (time.perf_counter_ns() - started) // 1_000_000)
        return {
            "schema_version": "1.0",
            "document_id": document_id,
            "document_type": hint,
            "confidence": 1.0 if hint != "unknown" else 0.0,
            "method": (hint_source or "explicit").strip().lower() or "explicit",
            "model": "rpd-rules-v1",
            "model_version": "1.0.0",
            "alternatives": [],
            "latency_ms": latency_ms,
            "requires_review": hint == "unknown",
        }

    text = build_document_text(pages)
    ranked = _rank(text)
    top_type, top_score = ranked[0]
    second_score = ranked[1][1] if len(ranked) > 1 else 0.0
    margin = top_score - second_score

    confident = top_score >= 4.0 and (margin >= 2.0 or top_score >= 9.0)
    if confident:
        confidence = min(0.995, 0.60 + min(top_score, 10.0) * 0.03 + min(margin, 5.0) * 0.02)
        document_type = top_type
    else:
        confidence = min(0.69, max(0.0, top_score / 12.0))
        document_type = "unknown"

    latency_ms = max(0, (time.perf_counter_ns() - started) // 1_000_000)
    return {
        "schema_version": "1.0",
        "document_id": document_id,
        "document_type": document_type,
        "confidence": round(confidence, 5),
        "method": "deterministic_rules",
        "model": "rpd-rules-v1",
        "model_version": "1.0.0",
        "alternatives": _alternatives(ranked),
        "latency_ms": latency_ms,
        "requires_review": document_type == "unknown" or confidence < 0.85,
    }
