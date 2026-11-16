from __future__ import annotations

import re
from typing import Any

from app.extraction.invoice import (
    extract_invoice_document,
    normalize_date,
    normalize_decimal,
    normalize_header,
    parse_table_html,
)


SUPPORTED_FINANCIAL_TYPES = {"invoice", "supplier_statement", "bank_statement"}

STATEMENT_SCHEMAS: dict[str, dict[str, Any]] = {
    "supplier_statement": {
        "required": ["supplier_name", "closing_balance"],
        "fields": {
            "supplier_name": {"type": "string", "aliases": ["supplier", "vendor", "supplier name"]},
            "account_number": {"type": "identifier", "aliases": ["account number", "account no", "acct no"]},
            "statement_date": {"type": "date", "aliases": ["statement date", "date"]},
            "currency": {"type": "currency", "aliases": ["currency"]},
            "opening_balance": {
                "type": "decimal",
                "aliases": ["opening balance", "previous balance", "balance brought forward"],
            },
            "closing_balance": {
                "type": "decimal",
                "aliases": ["closing balance", "current balance", "balance due"],
            },
        },
        "table": {
            "name": "transactions",
            "aliases": {
                "date": ["date", "transaction date", "posting date"],
                "invoice_number": ["invoice no", "invoice number", "invoice", "document no", "reference"],
                "description": ["description", "details", "particulars"],
                "debit": ["debit", "charges", "amount due"],
                "credit": ["credit", "payment", "payments"],
                "balance": ["balance", "running balance"],
            },
            "types": {
                "date": "date",
                "debit": "decimal",
                "credit": "decimal",
                "balance": "decimal",
            },
        },
    },
    "bank_statement": {
        "required": ["account_number", "closing_balance"],
        "fields": {
            "institution_name": {"type": "string", "aliases": ["bank", "bank name", "institution"]},
            "account_name": {"type": "string", "aliases": ["account name", "account holder", "customer name"]},
            "account_number": {"type": "identifier", "aliases": ["account number", "account no", "acct no"]},
            "statement_date": {"type": "date", "aliases": ["statement date", "as of"]},
            "currency": {"type": "currency", "aliases": ["currency"]},
            "opening_balance": {
                "type": "decimal",
                "aliases": ["opening balance", "beginning balance", "previous balance"],
            },
            "closing_balance": {
                "type": "decimal",
                "aliases": ["closing balance", "ending balance", "current balance"],
            },
        },
        "table": {
            "name": "transactions",
            "aliases": {
                "date": ["date", "transaction date", "posting date", "value date"],
                "description": ["description", "details", "transaction", "particulars"],
                "reference": ["reference", "ref", "reference no", "transaction id"],
                "debit": ["debit", "withdrawal", "withdrawals"],
                "credit": ["credit", "deposit", "deposits"],
                "balance": ["balance", "running balance"],
            },
            "types": {
                "date": "date",
                "debit": "decimal",
                "credit": "decimal",
                "balance": "decimal",
            },
        },
    },
}


def _bbox_rect(bbox: list[float]) -> tuple[float, float, float, float] | None:
    if len(bbox) == 4:
        x1, y1, x2, y2 = (float(v) for v in bbox)
        return min(x1, x2), min(y1, y2), max(x1, x2), max(y1, y2)
    if len(bbox) >= 8 and len(bbox) % 2 == 0:
        xs = [float(v) for v in bbox[0::2]]
        ys = [float(v) for v in bbox[1::2]]
        return min(xs), min(ys), max(xs), max(ys)
    return None


def _normalize_value(value: str, data_type: str) -> str | None:
    value = value.strip()
    if not value:
        return None
    if data_type == "decimal":
        return normalize_decimal(value)
    if data_type == "date":
        return normalize_date(value)
    if data_type == "currency":
        upper = value.upper()
        for code, symbols in {
            "PHP": ("PHP", "₱"),
            "USD": ("USD", "$"),
            "EUR": ("EUR", "€"),
            "GBP": ("GBP", "£"),
        }.items():
            if any(symbol in upper or symbol in value for symbol in symbols):
                return code
        return upper[:3] if len(upper) >= 3 else upper
    return " ".join(value.split())


def _field(value: str, raw: str, block: dict[str, Any], data_type: str, method: str) -> dict[str, Any]:
    return {
        "raw": raw,
        "value": value,
        "data_type": data_type,
        "evidence": {
            "page": int(block.get("_page", 0) or 0),
            "block_ids": [str(block.get("block_id", ""))],
            "bbox": list(block.get("bbox") or []),
            "method": method,
            "confidence": float(block.get("confidence", 0.0)),
        },
    }


def _inline_candidate(
    blocks: list[dict[str, Any]],
    aliases: list[str],
    data_type: str,
) -> dict[str, Any] | None:
    # Prefer explicit label/value separators before relaxed matching. This
    # prevents headings such as "BANK STATEMENT" from being interpreted as
    # the value of the short alias "bank" when "Bank: Metro Bank" exists.
    for require_separator in (True, False):
        for alias in aliases:
            escaped = re.escape(alias).replace(r"\ ", r"\s+")
            separator = r"\s*[:#-]\s*" if require_separator else r"\s+"
            pattern = re.compile(rf"(?:^|\b){escaped}{separator}(.+)$", re.IGNORECASE)
            for block in blocks:
                text = str(block.get("text", "")).strip()
                match = pattern.search(text)
                if not match:
                    continue
                raw_value = match.group(1).strip()
                value = _normalize_value(raw_value, data_type)
                if value:
                    return _field(value, text, block, data_type, "schema_alias")
    return None


def _neighbor_candidate(
    blocks: list[dict[str, Any]],
    aliases: list[str],
    data_type: str,
) -> dict[str, Any] | None:
    normalized_aliases = {normalize_header(alias) for alias in aliases}
    for label in blocks:
        if normalize_header(str(label.get("text", ""))) not in normalized_aliases:
            continue
        label_rect = _bbox_rect(list(label.get("bbox") or []))
        if label_rect is None:
            continue
        lx1, ly1, lx2, ly2 = label_rect
        label_height = max(1.0, ly2 - ly1)
        best: tuple[float, dict[str, Any]] | None = None
        for candidate in blocks:
            if candidate is label:
                continue
            rect = _bbox_rect(list(candidate.get("bbox") or []))
            if rect is None:
                continue
            cx1, cy1, cx2, cy2 = rect
            same_row = abs(((cy1 + cy2) / 2) - ((ly1 + ly2) / 2)) <= label_height * 1.25
            if not same_row or cx1 < lx2 - 2:
                continue
            distance = max(0.0, cx1 - lx2)
            if best is None or distance < best[0]:
                best = (distance, candidate)
        if best is None:
            continue
        candidate = best[1]
        raw_value = str(candidate.get("text", "")).strip()
        value = _normalize_value(raw_value, data_type)
        if value:
            return _field(value, raw_value, candidate, data_type, "schema_geometry")
    return None


def _detect_currency(blocks: list[dict[str, Any]]) -> dict[str, Any] | None:
    for block in blocks:
        text = str(block.get("text", ""))
        upper = text.upper()
        for code, symbols in {
            "PHP": ("PHP", "₱"),
            "USD": ("USD", "$"),
            "EUR": ("EUR", "€"),
            "GBP": ("GBP", "£"),
        }.items():
            if any(symbol in upper or symbol in text for symbol in symbols):
                return _field(code, text, block, "currency", "currency_detection")
    return None


def _extract_period(blocks: list[dict[str, Any]]) -> tuple[dict[str, Any] | None, dict[str, Any] | None]:
    pattern = re.compile(
        r"(?:statement\s*period|period)\s*[:#-]?\s*"
        r"([A-Z0-9,./-]+(?:\s+[A-Z0-9,./-]+)?)\s*(?:to|through|[-–])\s*"
        r"([A-Z0-9,./-]+(?:\s+[A-Z0-9,./-]+)?)",
        re.IGNORECASE,
    )
    for block in blocks:
        text = str(block.get("text", "")).strip()
        match = pattern.search(text)
        if not match:
            continue
        start = normalize_date(match.group(1).strip())
        end = normalize_date(match.group(2).strip())
        start_field = _field(start, text, block, "date", "statement_period") if start else None
        end_field = _field(end, text, block, "date", "statement_period") if end else None
        return start_field, end_field
    return None, None


def _map_table_header(value: str, aliases: dict[str, list[str]]) -> str | None:
    normalized = normalize_header(value)
    for target, candidates in aliases.items():
        if normalized == normalize_header(target):
            return target
        if normalized in {normalize_header(alias) for alias in candidates}:
            return target
    return None


def _extract_table_rows(
    table_html: str | None,
    *,
    page: int,
    table_schema: dict[str, Any],
) -> list[dict[str, Any]]:
    rows = parse_table_html(table_html)
    if not rows:
        return []
    aliases = dict(table_schema.get("aliases") or {})
    types = dict(table_schema.get("types") or {})

    header_index: int | None = None
    header_map: dict[int, str] = {}
    for index, row in enumerate(rows[:8]):
        mapped = {
            column: target
            for column, cell in enumerate(row)
            if (target := _map_table_header(cell, aliases))
        }
        if len(mapped) >= 2:
            header_index = index
            header_map = mapped
            break
    if header_index is None:
        return []

    output: list[dict[str, Any]] = []
    for row in rows[header_index + 1 :]:
        item: dict[str, Any] = {"page": page}
        for column, target in header_map.items():
            if column >= len(row):
                continue
            raw = row[column].strip()
            if not raw:
                continue
            value = _normalize_value(raw, str(types.get(target, "string"))) or raw
            item[target] = value
        if len(item) > 1:
            output.append(item)
    return output


def extract_statement_document(
    document_id: str,
    document_type: str,
    pages: list[dict[str, Any]],
) -> dict[str, Any]:
    schema = STATEMENT_SCHEMAS[document_type]
    blocks: list[dict[str, Any]] = []
    transactions: list[dict[str, Any]] = []

    for page_data in sorted(pages, key=lambda value: int(value.get("page", 0))):
        page = int(page_data.get("page", 0))
        page_blocks: list[dict[str, Any]] = []
        for source in page_data.get("ocr_blocks") or []:
            block = dict(source)
            block["_page"] = page
            page_blocks.append(block)
        blocks.extend(page_blocks)
        transactions.extend(
            _extract_table_rows(
                page_data.get("table_html"),
                page=page,
                table_schema=schema["table"],
            )
        )

    fields: dict[str, Any] = {}
    for field_name, definition in schema["fields"].items():
        aliases = list(definition.get("aliases") or [])
        data_type = str(definition.get("type", "string"))
        candidate = _inline_candidate(blocks, aliases, data_type)
        if candidate is None:
            candidate = _neighbor_candidate(blocks, aliases, data_type)
        if candidate is not None:
            fields[field_name] = candidate

    if "currency" in schema["fields"] and "currency" not in fields:
        currency = _detect_currency(blocks)
        if currency is not None:
            fields["currency"] = currency

    period_start, period_end = _extract_period(blocks)
    if period_start is not None:
        fields["period_start"] = period_start
    if period_end is not None:
        fields["period_end"] = period_end

    required = list(schema.get("required") or [])
    missing = [name for name in required if not fields.get(name)]
    low_confidence = [
        name
        for name, field in fields.items()
        if float((field.get("evidence") or {}).get("confidence", 0.0)) < 0.70
    ]

    return {
        "schema_version": "1.0",
        "document_id": document_id,
        "document_type": document_type,
        "schema": {"name": document_type, "version": "1.0"},
        "fields": fields,
        "tables": {"transactions": transactions},
        "validation": {
            "missing_required_fields": missing,
            "low_confidence_fields": low_confidence,
            "transaction_count": len(transactions),
        },
        "requires_review": bool(missing or low_confidence),
    }


def extract_financial_document(
    document_id: str,
    document_type: str,
    pages: list[dict[str, Any]],
    mapping_config: dict[str, Any] | None = None,
) -> dict[str, Any]:
    document_type = document_type.strip().lower()
    if document_type not in SUPPORTED_FINANCIAL_TYPES:
        raise ValueError(f"unsupported financial document type: {document_type}")
    if document_type == "invoice":
        return extract_invoice_document(document_id, pages, mapping_config=mapping_config)
    return extract_statement_document(document_id, document_type, pages)
