from __future__ import annotations

import hashlib
import json
import math
import re
from pathlib import Path
from typing import Any


def normalize_text(value: str) -> str:
    return " ".join(re.sub(r"[^a-z0-9]+", " ", value.lower()).split())


def bbox_rect(bbox: list[float]) -> tuple[float, float, float, float] | None:
    if len(bbox) == 4:
        x1, y1, x2, y2 = (float(value) for value in bbox)
        return min(x1, x2), min(y1, y2), max(x1, x2), max(y1, y2)
    if len(bbox) >= 8 and len(bbox) % 2 == 0:
        xs = [float(value) for value in bbox[0::2]]
        ys = [float(value) for value in bbox[1::2]]
        return min(xs), min(ys), max(xs), max(ys)
    return None


def page_dimensions(page: dict[str, Any]) -> tuple[int, int]:
    width = int(page.get("image_width") or 0)
    height = int(page.get("image_height") or 0)
    if width > 0 and height > 0:
        return width, height

    image_path = str(page.get("image_path") or "").strip()
    if image_path:
        try:
            from PIL import Image

            with Image.open(Path(image_path)) as image:
                return int(image.width), int(image.height)
        except (ImportError, OSError, ValueError):
            pass

    max_x = 0.0
    max_y = 0.0
    for block in page.get("ocr_blocks") or []:
        rect = bbox_rect(list(block.get("bbox") or []))
        if rect is None:
            continue
        max_x = max(max_x, rect[2])
        max_y = max(max_y, rect[3])
    return max(1, int(math.ceil(max_x))), max(1, int(math.ceil(max_y)))


def normalized_bbox(bbox: list[float], width: int, height: int) -> list[float]:
    rect = bbox_rect(bbox)
    if rect is None or width <= 0 or height <= 0:
        return []
    x1, y1, x2, y2 = rect
    return [
        round(max(0.0, min(1.0, x1 / width)), 5),
        round(max(0.0, min(1.0, y1 / height)), 5),
        round(max(0.0, min(1.0, x2 / width)), 5),
        round(max(0.0, min(1.0, y2 / height)), 5),
    ]


def _alias_match(text: str, aliases: list[str]) -> bool:
    normalized = normalize_text(text)
    for alias in aliases:
        candidate = normalize_text(alias)
        if not candidate:
            continue
        if normalized == candidate or normalized.startswith(candidate + " "):
            return True
    return False


def build_layout_fingerprint(
    document_type: str,
    pages: list[dict[str, Any]],
    field_aliases: dict[str, list[str]],
    table_aliases: dict[str, list[str]],
) -> dict[str, Any]:
    anchors: list[dict[str, Any]] = []
    table_headers: set[str] = set()
    orientations: list[str] = []
    aspect_ratios: list[float] = []

    for page in sorted(pages, key=lambda item: int(item.get("page", 0)))[:3]:
        width, height = page_dimensions(page)
        orientation = "landscape" if width > height else "portrait"
        orientations.append(orientation)
        aspect_ratios.append(round(width / max(1.0, float(height)), 4))

        for block in page.get("ocr_blocks") or []:
            text = str(block.get("text") or "")
            rect = normalized_bbox(list(block.get("bbox") or []), width, height)
            if not rect:
                continue
            cx = round((rect[0] + rect[2]) / 2.0, 4)
            cy = round((rect[1] + rect[3]) / 2.0, 4)

            for field_name, aliases in field_aliases.items():
                if _alias_match(text, aliases):
                    anchors.append({
                        "field": field_name,
                        "page": int(page.get("page", 0)),
                        "x": cx,
                        "y": cy,
                    })
                    break

            for header_name, aliases in table_aliases.items():
                if _alias_match(text, [header_name, *aliases]):
                    table_headers.add(header_name)
                    break

    anchors.sort(key=lambda value: (value["page"], value["field"], value["y"], value["x"]))
    fingerprint = {
        "schema_version": "1.0",
        "document_type": document_type,
        "orientation": orientations[0] if orientations else "unknown",
        "aspect_ratio": aspect_ratios[0] if aspect_ratios else 0.0,
        "anchors": anchors[:32],
        "table_headers": sorted(table_headers),
    }
    encoded = json.dumps(fingerprint, sort_keys=True, separators=(",", ":")).encode("utf-8")
    fingerprint["fingerprint_hash"] = hashlib.sha256(encoded).hexdigest()
    return fingerprint


def _anchor_score(current: list[dict[str, Any]], known: list[dict[str, Any]]) -> float:
    if not current or not known:
        return 0.0

    by_field: dict[str, list[dict[str, Any]]] = {}
    for anchor in known:
        by_field.setdefault(str(anchor.get("field") or ""), []).append(anchor)

    total = 0.0
    matched = 0
    for anchor in current:
        candidates = by_field.get(str(anchor.get("field") or ""), [])
        if not candidates:
            continue
        best = 0.0
        for candidate in candidates:
            dx = abs(float(anchor.get("x", 0.0)) - float(candidate.get("x", 0.0)))
            dy = abs(float(anchor.get("y", 0.0)) - float(candidate.get("y", 0.0)))
            distance = math.sqrt(dx * dx + dy * dy)
            best = max(best, max(0.0, 1.0 - distance / 0.25))
        total += best
        matched += 1

    coverage = matched / max(1, len({str(item.get("field") or "") for item in current}))
    return min(1.0, (total / max(1, matched)) * coverage) if matched else 0.0


def _jaccard(left: list[str], right: list[str]) -> float:
    a = {str(value) for value in left if str(value)}
    b = {str(value) for value in right if str(value)}
    if not a and not b:
        return 1.0
    if not a or not b:
        return 0.0
    return len(a & b) / len(a | b)


def score_layout(current: dict[str, Any], known: dict[str, Any]) -> float:
    if current.get("document_type") != known.get("document_type"):
        return 0.0

    score = 0.0
    if current.get("orientation") == known.get("orientation"):
        score += 0.10

    current_ratio = float(current.get("aspect_ratio") or 0.0)
    known_ratio = float(known.get("aspect_ratio") or 0.0)
    if current_ratio > 0 and known_ratio > 0:
        relative = abs(current_ratio - known_ratio) / max(current_ratio, known_ratio)
        score += 0.10 * max(0.0, 1.0 - relative / 0.20)

    score += 0.55 * _anchor_score(
        list(current.get("anchors") or []),
        list(known.get("anchors") or []),
    )
    score += 0.25 * _jaccard(
        list(current.get("table_headers") or []),
        list(known.get("table_headers") or []),
    )
    return round(min(1.0, score), 5)


def match_layout_profile(
    fingerprint: dict[str, Any],
    profiles: list[dict[str, Any]],
    threshold: float = 0.72,
) -> dict[str, Any]:
    best_profile: dict[str, Any] | None = None
    best_score = 0.0

    for profile in profiles:
        known = profile.get("fingerprint") or profile.get("fingerprint_json") or {}
        score = score_layout(fingerprint, known)
        if score > best_score:
            best_score = score
            best_profile = profile

    matched = best_profile is not None and best_score >= threshold
    return {
        "schema_version": "1.0",
        "fingerprint": fingerprint,
        "fingerprint_hash": fingerprint.get("fingerprint_hash", ""),
        "matched": matched,
        "matched_profile_id": str(best_profile.get("id") or "") if matched else "",
        "matched_profile_name": str(best_profile.get("name") or "") if matched else "",
        "score": best_score,
        "config": dict(best_profile.get("config") or best_profile.get("config_json") or {}) if matched else {},
    }
