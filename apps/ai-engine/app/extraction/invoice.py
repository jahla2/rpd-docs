from __future__ import annotations

import re
from dataclasses import dataclass
from datetime import datetime
from decimal import Decimal, InvalidOperation
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


def map_header(value: str) -> str | None:
    normalized = normalize_header(value)
    for target, aliases in HEADER_ALIASES.items():
        if normalized == target.replace("_", " ") or normalized in aliases:
            return target
    return None


def normalize_decimal(value: str) -> str | None:
    cleaned = (
        value.replace(",", "")
        .replace("₱", "")
        .replace("$", "")
        .replace("€", "")
        .replace("PHP", "")
        .replace("USD", "")
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


def extract_line_items(table_html: str | None) -> tuple[list[dict], dict]:
    rows = parse_table_html(table_html)
    if not rows:
        return [], {"header_row": None, "mapped_headers": {}}

    header_index: int | None = None
    header_map: dict[int, str] = {}

    for index, row in enumerate(rows[:6]):
        mapped = {i: target for i, cell in enumerate(row) if (target := map_header(cell))}
        if len(mapped) >= 2 and ("description" in mapped.values() or "line_total" in mapped.values()):
            header_index = index
            header_map = mapped
            break

    if header_index is None:
        return [], {"header_row": None, "mapped_headers": {}}

    items: list[dict] = []
    for row in rows[header_index + 1 :]:
        item: dict[str, str] = {}
        for column_index, target in header_map.items():
            if column_index >= len(row):
                continue
            value = row[column_index].strip()
            if not value:
                continue
            if target in {"quantity", "unit_price", "tax", "line_total"}:
                normalized = normalize_decimal(value)
                item[target] = normalized if normalized is not None else value
            else:
                item[target] = value

        if item and any(item.get(key) for key in ("description", "item_code", "line_total")):
            items.append(item)

    return items, {
        "header_row": header_index,
        "mapped_headers": {str(i): target for i, target in header_map.items()},
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


INVOICE_NUMBER = re.compile(
    r"(?:invoice\s*(?:no|number|#|ref)|inv\s*(?:no|#|ref))\s*[:#-]?\s*([A-Z0-9][A-Z0-9\-/]*)",
    re.IGNORECASE,
)
INVOICE_DATE = re.compile(
    r"(?:invoice\s*date|date)\s*[:#-]?\s*([A-Z0-9,./-]+(?:\s+[A-Z0-9,./-]+)?)",
    re.IGNORECASE,
)
GRAND_TOTAL = re.compile(
    r"(?:grand\s*total|invoice\s*total|total\s*due|amount\s*due|total)\s*[:#-]?\s*(?:PHP|USD|₱|\$)?\s*([0-9][0-9,]*(?:\.[0-9]{1,4})?)",
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


def _supplier_candidate(blocks: list[dict]) -> _Candidate | None:
    for block in blocks[:8]:
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


def extract_invoice(document_id: str, page: int, ocr_blocks: list[dict], table_html: str | None) -> dict:
    invoice_number = _search(ocr_blocks, INVOICE_NUMBER)
    invoice_date = _search(ocr_blocks, INVOICE_DATE, normalize_date)
    grand_total = _search(ocr_blocks, GRAND_TOTAL, normalize_decimal)
    currency = _detect_currency(ocr_blocks)
    supplier = _supplier_candidate(ocr_blocks)

    items, table_meta = extract_line_items(table_html)

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
        "currency": _field(currency, page, "deterministic_currency"),
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
