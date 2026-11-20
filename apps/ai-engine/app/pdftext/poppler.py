from __future__ import annotations

from dataclasses import dataclass
from html.parser import HTMLParser
from pathlib import Path
from typing import Any


@dataclass(slots=True)
class ParsedPage:
    width: float
    height: float
    lines: list[dict[str, Any]]


class _BBoxParser(HTMLParser):
    def __init__(self) -> None:
        super().__init__(convert_charrefs=True)
        self.page_width = 0.0
        self.page_height = 0.0
        self.lines: list[dict[str, Any]] = []
        self._line: dict[str, Any] | None = None
        self._word: dict[str, Any] | None = None

    @staticmethod
    def _attrs(attrs) -> dict[str, str]:
        return {str(k): str(v) for k, v in attrs}

    def handle_starttag(self, tag: str, attrs) -> None:
        tag = tag.lower()
        data = self._attrs(attrs)

        if tag == "page":
            self.page_width = float(data.get("width", "0") or 0)
            self.page_height = float(data.get("height", "0") or 0)
        elif tag == "line":
            self._line = {
                "x_min": float(data.get("xmin", data.get("xMin", "0")) or 0),
                "y_min": float(data.get("ymin", data.get("yMin", "0")) or 0),
                "x_max": float(data.get("xmax", data.get("xMax", "0")) or 0),
                "y_max": float(data.get("ymax", data.get("yMax", "0")) or 0),
                "words": [],
            }
        elif tag == "word" and self._line is not None:
            self._word = {"text": []}

    def handle_data(self, data: str) -> None:
        if self._word is not None:
            self._word["text"].append(data)

    def handle_endtag(self, tag: str) -> None:
        tag = tag.lower()
        if tag == "word" and self._word is not None and self._line is not None:
            text = "".join(self._word["text"]).strip()
            if text:
                self._line["words"].append(text)
            self._word = None
        elif tag == "line" and self._line is not None:
            text = " ".join(self._line["words"]).strip()
            if text:
                self.lines.append({
                    "text": text,
                    "x_min": self._line["x_min"],
                    "y_min": self._line["y_min"],
                    "x_max": self._line["x_max"],
                    "y_max": self._line["y_max"],
                })
            self._line = None


def parse_bbox_layout(layout_path: str, image_path: str, document_id: str, page: int) -> dict:
    layout_file = Path(layout_path)
    image_file = Path(image_path)

    if not layout_file.is_file():
        raise ValueError("layout_path does not exist")
    if not image_file.is_file():
        raise ValueError("image_path does not exist")

    parser = _BBoxParser()
    parser.feed(layout_file.read_text(encoding="utf-8", errors="replace"))

    if parser.page_width <= 0 or parser.page_height <= 0:
        raise ValueError("Poppler bbox layout did not contain valid page dimensions")

    try:
        from PIL import Image
    except ImportError as exc:
        raise RuntimeError("Pillow is required for Poppler coordinate scaling") from exc

    with Image.open(image_file) as image:
        image_width, image_height = image.size

    scale_x = image_width / parser.page_width
    scale_y = image_height / parser.page_height

    blocks: list[dict] = []
    for index, line in enumerate(parser.lines, start=1):
        x1 = float(line["x_min"]) * scale_x
        y1 = float(line["y_min"]) * scale_y
        x2 = float(line["x_max"]) * scale_x
        y2 = float(line["y_max"]) * scale_y
        blocks.append({
            "block_id": f"p{page:03d}-t{index:04d}",
            "text": str(line["text"]),
            "bbox": [x1, y1, x2, y1, x2, y2, x1, y2],
            "confidence": 1.0,
        })

    return {
        "schema_version": "1.0",
        "document_id": document_id,
        "page": page,
        "engine": "poppler_text",
        "model": None,
        "image_width": image_width,
        "image_height": image_height,
        "blocks": blocks,
    }
