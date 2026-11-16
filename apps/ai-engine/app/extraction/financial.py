from __future__ import annotations

import math
import re
from typing import Any

from app.extraction.invoice import (
    HEADER_ALIASES as INVOICE_TABLE_ALIASES,
    extract_invoice_document,
    normalize_date,
    normalize_decimal,
    normalize_header,
    parse_table_html,
)
from app.layout.profile import bbox_rect, normalized_bbox, page_dimensions


SUPPORTED_FINANCIAL_TYPES = {"invoice", "supplier_statement", "bank_statement"}

INVOICE_FIELD_ALIASES: dict[str, list[str]] = {
    "supplier_name": ["supplier", "vendor", "supplier name"],
    "invoice_number": ["invoice number", "invoice no", "invoice #", "invoice reference"],
    "invoice_date": ["invoice date", "date of invoice"],
    "due_date": ["due date", "payment due"],
    "po_number": ["po number", "purchase order", "po no"],
    "currency": ["currency"],
    "subtotal": ["subtotal", "sub total"],
    "tax": ["tax", "vat", "gst"],
    "grand_total": ["grand total", "amount due", "total due"],
}

INVOICE_FIELD_TYPES = {
    "supplier_name": "string",
    "invoice_number": "identifier",
    "invoice_date": "date",
    "due_date": "date",
    "po_number": "identifier",
    "currency": "currency",
    "subtotal": "decimal",
    "tax": "decimal",
    "grand_total": "decimal",
}

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


def field_aliases(document_type: str, layout_config: dict[str, Any] | None = None) -> dict[str, list[str]]:
    if document_type == "invoice":
        aliases = {name: list(values) for name, values in INVOICE_FIELD_ALIASES.items()}
    else:
        schema = STATEMENT_SCHEMAS.get(document_type) or {}
        aliases = {
            name: list(definition.get("aliases") or [])
            for name, definition in (schema.get("fields") or {}).items()
        }

    overrides = (layout_config or {}).get("field_aliases") or {}
    for name, values in overrides.items():
        aliases.setdefault(str(name), [])
        aliases[str(name)].extend(str(value) for value in values if str(value).strip())
    return aliases


def table_aliases(document_type: str, layout_config: dict[str, Any] | None = None) -> dict[str, list[str]]:
    if document_type == "invoice":
        aliases = {name: sorted(str(value) for value in values) for name, values in INVOICE_TABLE_ALIASES.items()}
    else:
        schema = STATEMENT_SCHEMAS.get(document_type) or {}
        aliases = {
            name: list(values)
            for name, values in ((schema.get("table") or {}).get("aliases") or {}).items()
        }

    overrides = (layout_config or {}).get("table_aliases") or {}
    for name, values in overrides.items():
        aliases.setdefault(str(name), [])
        aliases[str(name)].extend(str(value) for value in values if str(value).strip())
    return aliases


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


def _page_lookup(pages: list[dict[str, Any]]) -> dict[int, dict[str, Any]]:
    return {int(page.get("page", 0)): page for page in pages}


def _evidence_from_blocks(
    blocks: list[dict[str, Any]],
    *,
    data_type: str,
    method: str,
    pages: dict[int, dict[str, Any]],
) -> dict[str, Any]:
    page_number = int(blocks[0].get("_page", 0)) if blocks else 0
    rects = [bbox_rect(list(block.get("bbox") or [])) for block in blocks]
    rects = [rect for rect in rects if rect is not None]
    bbox: list[float] = []
    normalized: list[float] = []
    if rects:
        x1 = min(rect[0] for rect in rects)
        y1 = min(rect[1] for rect in rects)
        x2 = max(rect[2] for rect in rects)
        y2 = max(rect[3] for rect in rects)
        bbox = [x1, y1, x2, y2]
        page = pages.get(page_number) or {}
        width, height = page_dimensions(page)
        normalized = normalized_bbox(bbox, width, height)

    confidence = min((float(block.get("confidence", 0.0)) for block in blocks), default=0.0)
    return {
        "page": page_number,
        "block_ids": [str(block.get("block_id", "")) for block in blocks],
        "bbox": bbox,
        "bbox_normalized": normalized,
        "method": method,
        "confidence": confidence,
        "data_type": data_type,
    }


def _field_from_blocks(
    value: str,
    raw: str,
    blocks: list[dict[str, Any]],
    data_type: str,
    method: str,
    pages: dict[int, dict[str, Any]],
) -> dict[str, Any]:
    return {
        "raw": raw,
        "value": value,
        "data_type": data_type,
        "evidence": _evidence_from_blocks(
            blocks,
            data_type=data_type,
            method=method,
            pages=pages,
        ),
    }


def _inline_candidate(
    blocks: list[dict[str, Any]],
    aliases: list[str],
    data_type: str,
    pages: dict[int, dict[str, Any]],
) -> dict[str, Any] | None:
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
                    return _field_from_blocks(value, text, [block], data_type, "schema_alias", pages)
    return None


def _neighbor_candidate(
    blocks: list[dict[str, Any]],
    aliases: list[str],
    data_type: str,
    pages: dict[int, dict[str, Any]],
) -> dict[str, Any] | None:
    normalized_aliases = {normalize_header(alias) for alias in aliases}
    for label in blocks:
        if normalize_header(str(label.get("text", ""))) not in normalized_aliases:
            continue
        label_rect = bbox_rect(list(label.get("bbox") or []))
        if label_rect is None:
            continue
        lx1, ly1, lx2, ly2 = label_rect
        label_height = max(1.0, ly2 - ly1)
        label_width = max(1.0, lx2 - lx1)
        same_page = [block for block in blocks if int(block.get("_page", 0)) == int(label.get("_page", 0))]

        right_candidates: list[tuple[float, dict[str, Any]]] = []
        below_candidates: list[tuple[float, dict[str, Any]]] = []
        for candidate in same_page:
            if candidate is label:
                continue
            rect = bbox_rect(list(candidate.get("bbox") or []))
            if rect is None:
                continue
            cx1, cy1, cx2, cy2 = rect
            same_row = abs(((cy1 + cy2) / 2) - ((ly1 + ly2) / 2)) <= label_height * 1.25
            right_gap = max(0.0, cx1 - lx2)
            max_right_gap = max(120.0, label_width * 2.5)
            if same_row and cx1 >= lx2 - 2 and right_gap <= max_right_gap:
                right_candidates.append((right_gap, candidate))

            horizontal_overlap = max(0.0, min(lx2, cx2) - max(lx1, cx1))
            label_width = max(1.0, lx2 - lx1)
            aligned = horizontal_overlap / label_width >= 0.30 or abs(cx1 - lx1) <= label_width * 0.35
            gap = cy1 - ly2
            if aligned and 0 <= gap <= max(100.0, label_height * 5.0):
                below_candidates.append((gap, candidate))

        if right_candidates:
            candidate = min(right_candidates, key=lambda item: item[0])[1]
            raw_value = str(candidate.get("text", "")).strip()
            value = _normalize_value(raw_value, data_type)
            if value:
                return _field_from_blocks(value, raw_value, [candidate], data_type, "schema_geometry_right", pages)

        if below_candidates:
            below_candidates.sort(key=lambda item: (item[0], bbox_rect(list(item[1].get("bbox") or []))[0]))
            selected = [below_candidates[0][1]]
            if data_type == "string":
                first_rect = bbox_rect(list(selected[0].get("bbox") or []))
                if first_rect is not None:
                    last_bottom = first_rect[3]
                    for _, candidate in below_candidates[1:]:
                        if len(selected) >= 4:
                            break
                        rect = bbox_rect(list(candidate.get("bbox") or []))
                        if rect is None:
                            continue
                        gap = rect[1] - last_bottom
                        if gap < -2 or gap > label_height * 1.8:
                            continue
                        if abs(rect[0] - first_rect[0]) > max(40.0, (first_rect[2] - first_rect[0]) * 0.35):
                            continue
                        selected.append(candidate)
                        last_bottom = rect[3]

            raw_value = " ".join(str(block.get("text", "")).strip() for block in selected).strip()
            value = _normalize_value(raw_value, data_type)
            if value:
                return _field_from_blocks(value, raw_value, selected, data_type, "schema_geometry_below", pages)
    return None


def _profile_region_candidate(
    field_name: str,
    blocks: list[dict[str, Any]],
    data_type: str,
    pages: dict[int, dict[str, Any]],
    layout_config: dict[str, Any] | None,
) -> dict[str, Any] | None:
    region = ((layout_config or {}).get("field_regions") or {}).get(field_name) or {}
    expected = list(region.get("bbox_normalized") or [])
    if len(expected) != 4:
        return None

    expected_page = int(region.get("page") or 1)
    ex = (float(expected[0]) + float(expected[2])) / 2.0
    ey = (float(expected[1]) + float(expected[3])) / 2.0
    best: tuple[float, dict[str, Any]] | None = None

    for block in blocks:
        page_number = int(block.get("_page", 0))
        if page_number != expected_page:
            continue
        page = pages.get(page_number) or {}
        width, height = page_dimensions(page)
        normalized = normalized_bbox(list(block.get("bbox") or []), width, height)
        if len(normalized) != 4:
            continue
        cx = (normalized[0] + normalized[2]) / 2.0
        cy = (normalized[1] + normalized[3]) / 2.0
        distance = math.sqrt((cx - ex) ** 2 + (cy - ey) ** 2)
        if distance <= 0.12 and (best is None or distance < best[0]):
            best = (distance, block)

    if best is None:
        return None
    block = best[1]
    raw_value = str(block.get("text", "")).strip()
    prefix = normalize_header(str(region.get("raw_prefix") or ""))
    if prefix and normalize_header(raw_value).startswith(prefix):
        raw_value = re.sub(r"^[^:#-]*[:#-]?\s*", "", raw_value).strip()
    value = _normalize_value(raw_value, data_type)
    if not value:
        return None
    return _field_from_blocks(value, str(block.get("text", "")).strip(), [block], data_type, "layout_profile_region", pages)


def _detect_currency(
    blocks: list[dict[str, Any]],
    pages: dict[int, dict[str, Any]],
) -> dict[str, Any] | None:
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
                return _field_from_blocks(code, text, [block], "currency", "currency_detection", pages)
    return None


def _extract_period(
    blocks: list[dict[str, Any]],
    pages: dict[int, dict[str, Any]],
) -> tuple[dict[str, Any] | None, dict[str, Any] | None]:
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
        start_field = _field_from_blocks(start, text, [block], "date", "statement_period", pages) if start else None
        end_field = _field_from_blocks(end, text, [block], "date", "statement_period", pages) if end else None
        return start_field, end_field
    return None, None


def _map_table_header(value: str, aliases: dict[str, list[str]]) -> str | None:
    normalized = normalize_header(value)
    for target, candidates in aliases.items():
        names = [target, *candidates]
        if normalized in {normalize_header(alias) for alias in names}:
            return target
    return None


def _header_map(rows: list[list[str]], aliases: dict[str, list[str]]) -> tuple[int | None, dict[int, str]]:
    for index, row in enumerate(rows[:8]):
        mapped = {
            column: target
            for column, cell in enumerate(row)
            if (target := _map_table_header(cell, aliases))
        }
        if len(mapped) >= 2:
            return index, mapped

        if index + 1 < len(rows):
            next_row = rows[index + 1]
            combined: dict[int, str] = {}
            width = max(len(row), len(next_row))
            for column in range(width):
                upper = row[column] if column < len(row) else ""
                lower = next_row[column] if column < len(next_row) else ""
                candidate = " ".join(part for part in (upper, lower) if part).strip()
                target = _map_table_header(candidate, aliases) or _map_table_header(lower, aliases)
                if target:
                    combined[column] = target
            if len(combined) >= 2:
                return index + 1, combined
    return None, {}


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
    header_index, header_map = _header_map(rows, aliases)
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


def _augment_result_evidence(result: dict[str, Any], pages: list[dict[str, Any]]) -> None:
    page_map = _page_lookup(pages)
    blocks_by_page: dict[int, dict[str, dict[str, Any]]] = {}
    for page_number, page in page_map.items():
        blocks_by_page[page_number] = {
            str(block.get("block_id", "")): block
            for block in page.get("ocr_blocks") or []
        }

    if isinstance(result.get("fields"), dict):
        field_values = result["fields"].values()
    else:
        field_values = [
            result.get(name)
            for name in INVOICE_FIELD_TYPES
            if result.get(name)
        ]

    for field in field_values:
        if not isinstance(field, dict):
            continue
        evidence = field.get("evidence") or {}
        page_number = int(evidence.get("page") or 0)
        block_ids = [str(value) for value in evidence.get("block_ids") or []]
        blocks = [
            blocks_by_page.get(page_number, {}).get(block_id)
            for block_id in block_ids
        ]
        blocks = [block for block in blocks if block is not None]
        if not blocks:
            continue
        enriched = _evidence_from_blocks(
            blocks,
            data_type=str(field.get("data_type") or evidence.get("data_type") or "string"),
            method=str(evidence.get("method") or "deterministic_extraction"),
            pages=page_map,
        )
        evidence.update(enriched)
        field["evidence"] = evidence


def extract_statement_document(
    document_id: str,
    document_type: str,
    pages: list[dict[str, Any]],
    layout_config: dict[str, Any] | None = None,
) -> dict[str, Any]:
    schema = STATEMENT_SCHEMAS[document_type]
    page_map = _page_lookup(pages)
    blocks: list[dict[str, Any]] = []
    transactions: list[dict[str, Any]] = []
    table_metadata: list[dict[str, Any]] = []

    aliases_by_field = field_aliases(document_type, layout_config)
    table_schema = dict(schema["table"])
    table_schema["aliases"] = table_aliases(document_type, layout_config)

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
                table_schema=table_schema,
            )
        )
        table_metadata.append({
            "page": page,
            "cell_bboxes": page_data.get("table_cell_bboxes") or [],
            "logic_points": page_data.get("table_logic_points") or [],
        })

    fields: dict[str, Any] = {}
    for field_name, definition in schema["fields"].items():
        aliases = aliases_by_field.get(field_name) or []
        data_type = str(definition.get("type", "string"))
        candidate = _inline_candidate(blocks, aliases, data_type, page_map)
        if candidate is None:
            candidate = _neighbor_candidate(blocks, aliases, data_type, page_map)
        if candidate is None:
            candidate = _profile_region_candidate(
                field_name,
                blocks,
                data_type,
                page_map,
                layout_config,
            )
        if candidate is not None:
            fields[field_name] = candidate

    if "currency" in schema["fields"] and "currency" not in fields:
        currency = _detect_currency(blocks, page_map)
        if currency is not None:
            fields["currency"] = currency

    period_start, period_end = _extract_period(blocks, page_map)
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
        "schema": {"name": document_type, "version": "1.1"},
        "fields": fields,
        "tables": {"transactions": transactions},
        "table_metadata": table_metadata,
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
    layout_match: dict[str, Any] | None = None,
) -> dict[str, Any]:
    document_type = document_type.strip().lower()
    if document_type not in SUPPORTED_FINANCIAL_TYPES:
        raise ValueError(f"unsupported financial document type: {document_type}")

    layout_config = dict((layout_match or {}).get("config") or {})
    if document_type == "invoice":
        result = extract_invoice_document(document_id, pages, mapping_config=mapping_config)
    else:
        result = extract_statement_document(
            document_id,
            document_type,
            pages,
            layout_config=layout_config,
        )

    _augment_result_evidence(result, pages)
    result["layout"] = layout_match or {
        "matched": False,
        "matched_profile_id": "",
        "score": 0.0,
        "config": {},
    }
    return result
