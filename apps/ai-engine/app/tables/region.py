from __future__ import annotations

import re
from collections import defaultdict


HEADER_ALIASES = {
    "description": ("description", "item desc", "item description", "particulars", "product"),
    "item_code": ("item code", "sku", "product code"),
    "quantity": ("qty", "quantity", "units"),
    "unit": ("unit", "uom"),
    "unit_price": ("unit price", "unit cost", "price each", "unit rate"),
    "tax": ("tax", "vat", "gst"),
    "line_total": ("line total", "line amount", "extended amount", "total", "amount"),
}

FOOTER_TERMS = (
    "subtotal",
    "grand total",
    "amount due",
    "total due",
    "balance due",
)


def _normalize(text: str) -> str:
    return " ".join(re.sub(r"[^a-z0-9]+", " ", text.lower()).split())


def _bounds(bbox: list[float]) -> tuple[float, float, float, float] | None:
    if len(bbox) >= 8:
        xs = bbox[0::2]
        ys = bbox[1::2]
        return min(xs), min(ys), max(xs), max(ys)
    if len(bbox) == 4:
        x1, y1, x2, y2 = bbox
        return float(x1), float(y1), float(x2), float(y2)
    return None


def _targets(text: str, header_aliases: dict[str, tuple[str, ...] | list[str]] | None = None) -> set[str]:
    normalized = _normalize(text)
    matches: set[str] = set()
    padded = f" {normalized} "
    aliases_by_target = header_aliases or HEADER_ALIASES
    for target, aliases in aliases_by_target.items():
        for alias in aliases:
            alias_normalized = _normalize(alias)
            if normalized == alias_normalized or f" {alias_normalized} " in padded:
                matches.add(target)
                break
    return matches


def detect_table_region(
    blocks: list[dict],
    image_width: int,
    image_height: int,
    header_aliases: dict[str, tuple[str, ...] | list[str]] | None = None,
) -> list[int] | None:
    """Detect a probable line-item table region from OCR header geometry.

    This is deliberately deterministic. It requires at least two recognizable
    table-header concepts on approximately the same horizontal band.
    """

    if image_width <= 0 or image_height <= 0:
        return None

    candidates: list[dict] = []
    normalized_blocks: list[dict] = []

    for block in blocks:
        bounds = _bounds(list(block.get("bbox", [])))
        if bounds is None:
            continue
        x1, y1, x2, y2 = bounds
        text = str(block.get("text", ""))
        entry = {
            "text": text,
            "x1": x1,
            "y1": y1,
            "x2": x2,
            "y2": y2,
            "cy": (y1 + y2) / 2.0,
        }
        normalized_blocks.append(entry)

        targets = _targets(text, header_aliases)
        if targets:
            entry = dict(entry)
            entry["targets"] = targets
            candidates.append(entry)

    if not candidates:
        return None

    tolerance = max(20.0, image_height * 0.018)
    groups: dict[int, list[dict]] = defaultdict(list)

    for candidate in candidates:
        bucket = int(round(candidate["cy"] / tolerance))
        groups[bucket].append(candidate)

    best_group: list[dict] | None = None
    best_score = 0

    for group in groups.values():
        concepts: set[str] = set()
        for item in group:
            concepts.update(item["targets"])
        score = len(concepts)
        if score > best_score:
            best_score = score
            best_group = group

    if best_group is None or best_score < 2:
        return None

    header_top = min(item["y1"] for item in best_group)
    header_bottom = max(item["y2"] for item in best_group)
    header_left = min(item["x1"] for item in best_group)
    header_right = max(item["x2"] for item in best_group)

    x_margin = max(24.0, image_width * 0.025)
    y_margin = max(12.0, image_height * 0.008)

    x1 = max(0.0, header_left - x_margin)
    x2 = min(float(image_width), header_right + x_margin)
    y1 = max(0.0, header_top - y_margin)

    # Start by including all OCR content below the header, then cut at the
    # first totals/footer label when it clearly follows the item area.
    below = [b for b in normalized_blocks if b["cy"] >= header_bottom]
    if below:
        y2 = min(float(image_height), max(b["y2"] for b in below) + y_margin)
    else:
        y2 = min(float(image_height), header_bottom + image_height * 0.35)

    footer_candidates = []
    for block in below:
        normalized = _normalize(block["text"])
        if any(term in normalized for term in FOOTER_TERMS):
            if block["y1"] > header_bottom + max(80.0, image_height * 0.04):
                footer_candidates.append(block["y1"])

    if footer_candidates:
        y2 = min(y2, min(footer_candidates) - y_margin)

    if y2 - y1 < max(80.0, image_height * 0.05):
        return None

    # If header OCR spans only part of the table, widen modestly using nearby
    # row content while still avoiding unrelated page margins.
    row_blocks = [
        b for b in normalized_blocks
        if b["cy"] >= header_top and b["cy"] <= y2
    ]
    if row_blocks:
        content_left = min(b["x1"] for b in row_blocks)
        content_right = max(b["x2"] for b in row_blocks)
        x1 = max(0.0, min(x1, content_left - x_margin))
        x2 = min(float(image_width), max(x2, content_right + x_margin))

    return [
        int(round(x1)),
        int(round(y1)),
        int(round(x2)),
        int(round(y2)),
    ]


def translate_ocr_blocks(blocks: list[dict], region: list[int]) -> list[dict]:
    x1, y1, x2, y2 = region
    translated: list[dict] = []

    for block in blocks:
        bbox = list(block.get("bbox", []))
        bounds = _bounds(bbox)
        if bounds is None:
            continue
        bx1, by1, bx2, by2 = bounds
        center_x = (bx1 + bx2) / 2.0
        center_y = (by1 + by2) / 2.0
        if not (x1 <= center_x <= x2 and y1 <= center_y <= y2):
            continue

        adjusted = bbox[:]
        for i in range(0, len(adjusted), 2):
            adjusted[i] = float(adjusted[i]) - x1
            if i + 1 < len(adjusted):
                adjusted[i + 1] = float(adjusted[i + 1]) - y1

        translated.append({
            **block,
            "bbox": adjusted,
        })

    return translated
