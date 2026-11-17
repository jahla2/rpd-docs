from __future__ import annotations

import json
import re
from dataclasses import dataclass
from datetime import datetime
from decimal import Decimal, InvalidOperation, ROUND_HALF_UP
from html.parser import HTMLParser


HEADER_ALIASES: dict[str, set[str]] = {
    "description": {"description", "item", "item desc", "item description", "particulars", "product"},
    "item_code": {"item code", "sku", "product code", "code"},
    "quantity": {"qty", "quantity", "units"},
    "unit": {"unit", "uom"},
    "unit_price": {"unit price", "unit cost", "price each", "unit rate", "price"},
    "tax": {"tax", "vat", "gst"},
    "line_total": {"total", "amount", "line total", "line amount", "extended amount"},
}


def normalize_header(value: str) -> str:
    value = re.sub(r"[^a-z0-9]+", " ", value.lower().strip())
    return " ".join(value.split())


def _aliases(mapping_config: dict | None) -> dict[str, set[str]]:
    merged = {target: set(values) for target, values in HEADER_ALIASES.items()}
    if not mapping_config:
        return merged
    for column in mapping_config.get("columns") or []:
        target = str(column.get("target", "")).strip()
        if not target:
            continue
        merged.setdefault(target, set())
        for alias in column.get("source_aliases") or []:
            normalized = normalize_header(str(alias))
            if normalized:
                merged[target].add(normalized)
    return merged


def map_headers(value: str, mapping_config: dict | None = None) -> list[str]:
    normalized = normalize_header(value)
    targets: list[str] = []
    for target, aliases in _aliases(mapping_config).items():
        if normalized == target.replace("_", " ") or normalized in aliases:
            targets.append(target)
    return targets


def map_header(value: str, mapping_config: dict | None = None) -> str | None:
    targets = map_headers(value, mapping_config)
    return targets[0] if targets else None


def normalize_decimal(value: str) -> str | None:
    cleaned = (
        value.replace(",", "")
        .replace("₱", "")
        .replace("$", "")
        .replace("€", "")
        .replace("PHP", "")
        .replace("USD", "")
        .replace("EUR", "")
        .strip()
    )
    if cleaned == "":
        return None
    try:
        return format(Decimal(cleaned), "f")
    except InvalidOperation:
        return None


def normalize_date(value: str) -> str | None:
    candidate = value.strip().replace(".", "/")
    formats = (
        "%Y-%m-%d",
        "%d/%m/%Y",
        "%m/%d/%Y",
        "%d-%m-%Y",
        "%m-%d-%Y",
        "%d/%m/%y",
        "%m/%d/%y",
        "%d-%b-%Y",
        "%b %d, %Y",
        "%B %d, %Y",
    )
    for fmt in formats:
        try:
            return datetime.strptime(candidate, fmt).date().isoformat()
        except ValueError:
            continue
    return None


class _TableParser(HTMLParser):
    def __init__(self) -> None:
        super().__init__()
        self.rows: list[list[str]] = []
        self._row: list[str] | None = None
        self._cell: list[str] | None = None

    def handle_starttag(self, tag: str, attrs) -> None:
        tag = tag.lower()
        if tag == "tr":
            self._row = []
        elif tag in {"td", "th"} and self._row is not None:
            self._cell = []

    def handle_data(self, data: str) -> None:
        if self._cell is not None:
            self._cell.append(data)

    def handle_endtag(self, tag: str) -> None:
        tag = tag.lower()
        if tag in {"td", "th"} and self._cell is not None and self._row is not None:
            self._row.append(" ".join("".join(self._cell).split()))
            self._cell = None
        elif tag == "tr" and self._row is not None:
            if any(cell.strip() for cell in self._row):
                self.rows.append(self._row)
            self._row = None


def parse_table_html(html: str | None) -> list[list[str]]:
    if not html:
        return []
    parser = _TableParser()
    parser.feed(html)
    return parser.rows


def _combine_aliases(raw: object) -> list[str] | None:
    if not isinstance(raw, list) or not 1 <= len(raw) <= 8:
        return None
    aliases: list[str] = []
    seen: set[str] = set()
    for value in raw:
        if not isinstance(value, str):
            return None
        alias = value.strip()
        normalized = normalize_header(alias)
        if not normalized or len(alias.encode("utf-8")) > 200:
            return None
        if normalized not in seen:
            seen.add(normalized)
            aliases.append(alias)
    return aliases if aliases else None


def _compute_decimal(value: str) -> Decimal | None:
    if len(value) > 128:
        return None
    normalized = normalize_decimal(value)
    if normalized is None or len(normalized) > 128:
        return None
    try:
        return Decimal(normalized)
    except InvalidOperation:
        return None


def _apply_operations(
    value: str,
    operations: list[dict],
    context: dict[str, str] | None = None,
) -> tuple[str | None, bool]:
    current: str | None = value
    excluded = False
    for operation in operations:
        name = str(operation.get("operation", "")).strip().lower()
        config = operation.get("config") or {}
        if name == "exclude":
            excluded = True
            current = None
            break
        if name == "default" and (current is None or current.strip() == ""):
            current = str(config.get("value", ""))
        elif name == "normalize" and current is not None:
            current = " ".join(current.split())
            case = str(config.get("case", "")).lower()
            if case == "upper":
                current = current.upper()
            elif case == "lower":
                current = current.lower()
        elif name == "decimal" and current is not None:
            current = normalize_decimal(current)
        elif name == "date" and current is not None:
            current = normalize_date(current)
        elif name == "split" and current is not None:
            delimiter = config.get("delimiter")
            index = config.get("index")
            trim = config.get("trim", True)
            if (
                not isinstance(delimiter, str)
                or not delimiter
                or len(delimiter.encode("utf-8")) > 16
                or not isinstance(index, int)
                or isinstance(index, bool)
                or index < 0
                or index > 31
                or not isinstance(trim, bool)
            ):
                current = None
                continue
            parts = current.split(delimiter)
            if index >= len(parts):
                current = None
                continue
            current = parts[index]
            if trim:
                current = current.strip()
        elif name == "combine" and current is not None:
            aliases = _combine_aliases(config.get("with"))
            separator = config.get("separator", " ")
            skip_empty = config.get("skip_empty", False)
            if (
                aliases is None
                or not isinstance(separator, str)
                or len(separator.encode("utf-8")) > 16
                or not isinstance(skip_empty, bool)
            ):
                current = None
                continue

            parts = [current]
            source_context = context or {}
            failed = False
            for alias in aliases:
                additional = source_context.get(normalize_header(alias))
                if additional is None or additional.strip() == "":
                    if skip_empty:
                        continue
                    failed = True
                    break
                parts.append(additional.strip())
            current = None if failed else separator.join(parts)
        elif name == "compute" and current is not None:
            operator = str(config.get("operator", "")).strip().lower()
            operand = config.get("operand")
            scale = config.get("scale")
            if (
                operator not in {"add", "subtract", "multiply", "divide"}
                or not isinstance(operand, str)
                or not normalize_header(operand)
                or len(operand.encode("utf-8")) > 200
                or not isinstance(scale, int)
                or isinstance(scale, bool)
                or scale < 0
                or scale > 8
            ):
                current = None
                continue

            source_context = context or {}
            operand_value = source_context.get(normalize_header(operand))
            if operand_value is None:
                current = None
                continue
            left = _compute_decimal(current)
            right = _compute_decimal(operand_value)
            if left is None or right is None:
                current = None
                continue

            if operator == "add":
                result = left + right
            elif operator == "subtract":
                result = left - right
            elif operator == "multiply":
                result = left * right
            else:
                if right == 0:
                    current = None
                    continue
                result = left / right

            quantizer = Decimal(1).scaleb(-scale)
            try:
                result = result.quantize(quantizer, rounding=ROUND_HALF_UP)
            except InvalidOperation:
                current = None
                continue
            current = format(result, "f")
        elif name == "lookup":
            # Tenant registries are resolved by the orchestrator. Never emit
            # the raw lookup key as if it were the resolved identifier.
            current = None
    return current, excluded


def extract_line_items(table_html: str | None, mapping_config: dict | None = None) -> tuple[list[dict], dict]:
    rows = parse_table_html(table_html)
    if not rows:
        return [], {"header_row": None, "mapped_headers": {}}

    header_index: int | None = None
    header_map: dict[int, list[str]] = {}

    for index, row in enumerate(rows[:6]):
        mapped = {
            i: targets
            for i, cell in enumerate(row)
            if (targets := map_headers(cell, mapping_config))
        }
        flattened_targets = {
            target
            for targets in mapped.values()
            for target in targets
        }
        if len(mapped) >= 2 and (
            "description" in flattened_targets or "line_total" in flattened_targets
        ):
            header_index = index
            header_map = mapped
            break

    if header_index is None:
        return [], {"header_row": None, "mapped_headers": {}}

    header_row = rows[header_index]
    items: list[dict] = []
    for row in rows[header_index + 1 :]:
        item: dict[str, str] = {}
        row_context = {
            normalize_header(header): (
                row[column_index].strip() if column_index < len(row) else ""
            )
            for column_index, header in enumerate(header_row)
            if normalize_header(header)
        }
        for column_index, targets in header_map.items():
            if column_index >= len(row):
                continue
            value = row[column_index].strip()
            if not value:
                continue

            for target in targets:
                column_rule = None
                if mapping_config:
                    for candidate in mapping_config.get("columns") or []:
                        if candidate.get("target") == target:
                            column_rule = candidate
                            break

                operations = list((column_rule or {}).get("operations") or [])
                transformed, excluded = _apply_operations(
                    value,
                    operations,
                    row_context,
                )
                if excluded:
                    continue
                strict_transform = any(
                    str(operation.get("operation", "")).strip().lower()
                    in {"split", "combine", "compute", "lookup"}
                    for operation in operations
                )
                if strict_transform and transformed is None:
                    continue

                if target in {"quantity", "unit_price", "tax", "line_total"} and not operations:
                    normalized = normalize_decimal(value)
                    transformed = normalized if normalized is not None else value
                item[target] = transformed if transformed is not None else value

        if item and any(
            item.get(key)
            for key in ("description", "item_code", "line_total", "calculated_line_total")
        ):
            items.append(item)

    return items, {
        "header_row": header_index,
        "mapped_headers": {
            str(i): targets[0] if len(targets) == 1 else targets
            for i, targets in header_map.items()
        },
    }


@dataclass(slots=True)
class _Candidate:
    value: str
    block_id: str
    confidence: float
    raw: str


def _field(candidate: _Candidate | None, page: int, method: str = "deterministic_regex"):
    if candidate is None:
        return None
    return {
        "raw": candidate.raw,
        "value": candidate.value,
        "evidence": {
            "page": page,
            "block_ids": [candidate.block_id],
            "method": method,
            "confidence": candidate.confidence,
        },
    }


def _search(blocks: list[dict], pattern: re.Pattern[str], normalizer=None) -> _Candidate | None:
    for block in blocks:
        text = str(block.get("text", "")).strip()
        match = pattern.search(text)
        if not match:
            continue
        raw_value = match.group(match.lastindex or 1).strip()
        value = normalizer(raw_value) if normalizer else raw_value
        if value is None:
            continue
        return _Candidate(
            value=str(value),
            block_id=str(block.get("block_id", "")),
            confidence=float(block.get("confidence", 0.0)),
            raw=text,
        )
    return None


def _column_rule(mapping_config: dict | None, target: str) -> dict | None:
    if not mapping_config:
        return None
    for column in mapping_config.get("columns") or []:
        if str(column.get("target", "")).strip() == target:
            return column
    return None


def _extract_alias_value(blocks: list[dict], alias_text: str) -> str | None:
    alias_text = alias_text.strip()
    if not alias_text:
        return None
    escaped = re.escape(alias_text).replace(r"\ ", r"\s+")
    pattern = re.compile(
        rf"(?:^|\b){escaped}\s*[:#-]?\s*(.+)$",
        re.IGNORECASE,
    )
    for block in blocks:
        text = str(block.get("text", "")).strip()
        match = pattern.search(text)
        if match:
            value = match.group(1).strip()
            if value:
                return value
    return None


def _search_alias(
    blocks: list[dict],
    target: str,
    mapping_config: dict | None,
    normalizer=None,
) -> _Candidate | None:
    rule = _column_rule(mapping_config, target)
    if not rule:
        return None

    for alias in rule.get("source_aliases") or []:
        alias_text = str(alias).strip()
        if not alias_text:
            continue
        # Match the configured label at a word boundary and capture the
        # remaining label-value content. Whitespace is flexible but the alias
        # itself is escaped, so configuration cannot inject a regex.
        escaped = re.escape(alias_text).replace(r"\ ", r"\s+")
        pattern = re.compile(
            rf"(?:^|\b){escaped}\s*[:#-]?\s*(.+)$",
            re.IGNORECASE,
        )
        for block in blocks:
            text = str(block.get("text", "")).strip()
            match = pattern.search(text)
            if not match:
                continue
            raw_value = match.group(1).strip()
            operations = list(rule.get("operations") or [])
            operation_context: dict[str, str] = {}
            for operation in operations:
                operation_name = str(operation.get("operation", "")).strip().lower()
                operation_config = operation.get("config") or {}
                if operation_name == "combine":
                    aliases = _combine_aliases(operation_config.get("with"))
                    if aliases is None:
                        continue
                    for combine_alias in aliases:
                        additional = _extract_alias_value(blocks, combine_alias)
                        if additional is not None:
                            operation_context[normalize_header(combine_alias)] = additional
                elif operation_name == "compute":
                    operand = operation_config.get("operand")
                    if isinstance(operand, str) and normalize_header(operand):
                        additional = _extract_alias_value(blocks, operand)
                        if additional is not None:
                            operation_context[normalize_header(operand)] = additional

            transformed, excluded = _apply_operations(
                raw_value,
                operations,
                operation_context,
            )
            if excluded:
                return None
            strict_transform = any(
                str(operation.get("operation", "")).strip().lower()
                in {"split", "combine", "compute", "lookup"}
                for operation in operations
            )
            if strict_transform and transformed is None:
                continue
            value = transformed
            if value is None:
                value = normalizer(raw_value) if normalizer else raw_value
            elif normalizer and not rule.get("operations"):
                value = normalizer(value)
            if value is None or str(value).strip() == "":
                continue
            return _Candidate(
                value=str(value),
                block_id=str(block.get("block_id", "")),
                confidence=float(block.get("confidence", 0.0)),
                raw=text,
            )
    return None


INVOICE_NUMBER = re.compile(
    r"(?:invoice\s*(?:no|number|#|ref)|inv\s*(?:no|#|ref))\s*[:#-]?\s*([A-Z0-9][A-Z0-9\-/]*)",
    re.IGNORECASE,
)
INVOICE_DATE = re.compile(
    r"(?:invoice\s*date|^date)\s*[:#-]?\s*([A-Z0-9,./-]+(?:\s+[A-Z0-9,./-]+)?)",
    re.IGNORECASE,
)
DUE_DATE = re.compile(
    r"(?:due\s*date|payment\s*due)\s*[:#-]?\s*([A-Z0-9,./-]+(?:\s+[A-Z0-9,./-]+)?)",
    re.IGNORECASE,
)
PO_NUMBER = re.compile(
    r"(?:purchase\s*order|po)\s*(?:no|number|#)?\s*[:#-]?\s*([A-Z0-9][A-Z0-9\-/]*)",
    re.IGNORECASE,
)
SUBTOTAL = re.compile(
    r"(?:sub\s*total|subtotal)\s*[:#-]?\s*(?:PHP|USD|EUR|₱|\$|€)?\s*([0-9][0-9,]*(?:\.[0-9]{1,4})?)",
    re.IGNORECASE,
)
TAX_TOTAL = re.compile(
    r"(?:tax|vat|gst)\s*(?:total|amount)?\s*[:#-]?\s*(?:PHP|USD|EUR|₱|\$|€)?\s*([0-9][0-9,]*(?:\.[0-9]{1,4})?)",
    re.IGNORECASE,
)
GRAND_TOTAL = re.compile(
    r"(?:grand\s*total|invoice\s*total|total\s*due|amount\s*due|^total)\s*[:#-]?\s*(?:PHP|USD|EUR|₱|\$|€)?\s*([0-9][0-9,]*(?:\.[0-9]{1,4})?)",
    re.IGNORECASE,
)


def _detect_currency(blocks: list[dict]) -> _Candidate | None:
    for block in blocks:
        text = str(block.get("text", ""))
        upper = text.upper()
        if "PHP" in upper or "₱" in text:
            return _Candidate("PHP", str(block.get("block_id", "")), float(block.get("confidence", 0.0)), text)
        if "USD" in upper or "$" in text:
            return _Candidate("USD", str(block.get("block_id", "")), float(block.get("confidence", 0.0)), text)
        if "EUR" in upper or "€" in text:
            return _Candidate("EUR", str(block.get("block_id", "")), float(block.get("confidence", 0.0)), text)
    return None


def _supplier_candidate(blocks: list[dict], mapping_config: dict | None = None) -> _Candidate | None:
    configured = _search_alias(blocks, "supplier_name", mapping_config)
    if configured is not None:
        return configured
    for block in blocks[:10]:
        text = str(block.get("text", "")).strip()
        if len(text) < 3 or len(text) > 120:
            continue
        lower = text.lower()
        if any(term in lower for term in ("invoice", "date", "total", "tax", "vat", "qty", "quantity")):
            continue
        if sum(ch.isalpha() for ch in text) < 3:
            continue
        return _Candidate(text, str(block.get("block_id", "")), float(block.get("confidence", 0.0)), text)
    return None


def extract_invoice(
    document_id: str,
    page: int,
    ocr_blocks: list[dict],
    table_html: str | None,
    mapping_config: dict | None = None,
) -> dict:
    invoice_number = _search_alias(ocr_blocks, "invoice_number", mapping_config) or _search(ocr_blocks, INVOICE_NUMBER)
    invoice_date = _search_alias(ocr_blocks, "invoice_date", mapping_config, normalize_date) or _search(ocr_blocks, INVOICE_DATE, normalize_date)
    due_date = _search_alias(ocr_blocks, "due_date", mapping_config, normalize_date) or _search(ocr_blocks, DUE_DATE, normalize_date)
    po_number = _search_alias(ocr_blocks, "po_number", mapping_config) or _search(ocr_blocks, PO_NUMBER)
    subtotal = _search_alias(ocr_blocks, "subtotal", mapping_config, normalize_decimal) or _search(ocr_blocks, SUBTOTAL, normalize_decimal)
    tax = _search_alias(ocr_blocks, "tax", mapping_config, normalize_decimal) or _search(ocr_blocks, TAX_TOTAL, normalize_decimal)
    grand_total = _search_alias(ocr_blocks, "grand_total", mapping_config, normalize_decimal) or _search(ocr_blocks, GRAND_TOTAL, normalize_decimal)
    currency = _search_alias(
        ocr_blocks,
        "currency",
        mapping_config,
        lambda value: value.strip().upper() if value.strip().upper() in {"PHP", "USD", "EUR"} else None,
    ) or _detect_currency(ocr_blocks)
    supplier = _supplier_candidate(ocr_blocks, mapping_config)

    items, table_meta = extract_line_items(table_html, mapping_config)

    required = {
        "supplier_name": supplier,
        "invoice_number": invoice_number,
        "invoice_date": invoice_date,
        "currency": currency,
        "grand_total": grand_total,
    }
    missing = [name for name, candidate in required.items() if candidate is None]
    low_confidence = [
        name for name, candidate in required.items()
        if candidate is not None and candidate.confidence < 0.70
    ]

    return {
        "schema_version": "1.0",
        "document_id": document_id,
        "document_type": "invoice",
        "supplier_name": _field(supplier, page),
        "invoice_number": _field(invoice_number, page),
        "invoice_date": _field(invoice_date, page),
        "due_date": _field(due_date, page),
        "po_number": _field(po_number, page),
        "currency": _field(currency, page, "deterministic_currency"),
        "subtotal": _field(subtotal, page),
        "tax": _field(tax, page),
        "grand_total": _field(grand_total, page),
        "line_items": items,
        "table_mapping": table_meta,
        "validation": {
            "missing_required_fields": missing,
            "low_confidence_fields": low_confidence,
            "has_line_items": len(items) > 0,
        },
        "requires_review": bool(missing or low_confidence),
    }


def _field_confidence(field: dict | None) -> float:
    if not field:
        return -1.0
    evidence = field.get("evidence") or {}
    return float(evidence.get("confidence", 0.0))


def _field_page(field: dict | None) -> int:
    if not field:
        return 0
    evidence = field.get("evidence") or {}
    return int(evidence.get("page", 0))


def _pick_field(results: list[dict], name: str, prefer_later: bool = False):
    fields = [result.get(name) for result in results if result.get(name)]
    if not fields:
        return None
    fields.sort(
        key=lambda field: (
            _field_confidence(field),
            _field_page(field) if prefer_later else -_field_page(field),
        ),
        reverse=True,
    )
    return fields[0]


def extract_invoice_document(document_id: str, pages: list[dict], mapping_config: dict | None = None) -> dict:
    per_page: list[dict] = []
    all_items: list[dict] = []
    table_mappings: list[dict] = []

    for page_data in sorted(pages, key=lambda value: int(value.get("page", 0))):
        page = int(page_data["page"])
        result = extract_invoice(
            document_id=document_id,
            page=page,
            ocr_blocks=list(page_data.get("ocr_blocks") or []),
            table_html=page_data.get("table_html"),
            mapping_config=mapping_config,
        )
        per_page.append(result)
        table_mappings.append({"page": page, **result["table_mapping"]})
        for item in result.get("line_items") or []:
            all_items.append({"page": page, **item})

    deduped_items: list[dict] = []
    seen: set[str] = set()
    for item in all_items:
        signature = json.dumps(item, sort_keys=True, separators=(",", ":"))
        if signature not in seen:
            seen.add(signature)
            deduped_items.append(item)

    merged = {
        "schema_version": "1.0",
        "document_id": document_id,
        "document_type": "invoice",
        "supplier_name": _pick_field(per_page, "supplier_name"),
        "invoice_number": _pick_field(per_page, "invoice_number"),
        "invoice_date": _pick_field(per_page, "invoice_date"),
        "due_date": _pick_field(per_page, "due_date"),
        "po_number": _pick_field(per_page, "po_number"),
        "currency": _pick_field(per_page, "currency"),
        "subtotal": _pick_field(per_page, "subtotal", prefer_later=True),
        "tax": _pick_field(per_page, "tax", prefer_later=True),
        "grand_total": _pick_field(per_page, "grand_total", prefer_later=True),
        "line_items": deduped_items,
        "table_mappings": table_mappings,
        "page_count": len(pages),
    }

    required_names = ("supplier_name", "invoice_number", "invoice_date", "currency", "grand_total")
    missing = [name for name in required_names if not merged.get(name)]
    low_confidence = [
        name for name in required_names
        if merged.get(name) and _field_confidence(merged[name]) < 0.70
    ]
    merged["validation"] = {
        "missing_required_fields": missing,
        "low_confidence_fields": low_confidence,
        "has_line_items": len(deduped_items) > 0,
    }
    merged["requires_review"] = bool(missing or low_confidence)
    return merged
