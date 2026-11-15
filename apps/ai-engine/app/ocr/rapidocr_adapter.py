from __future__ import annotations

from typing import Any

from .base import OCRBlock, OCREngine, OCRPage


def normalize_bbox(box: Any) -> list[float]:
    if box is None:
        return [0.0, 0.0, 0.0, 0.0]

    values: list[float] = []
    for item in box:
        if isinstance(item, (list, tuple)):
            values.extend(float(v) for v in item)
        else:
            values.append(float(item))
    return values


class RapidOCREngine(OCREngine):
    def __init__(self) -> None:
        try:
            from rapidocr import RapidOCR
        except ImportError as exc:
            raise RuntimeError(
                "RapidOCR is not installed. Run: pip install -e '.[ocr]'"
            ) from exc
        self._engine = RapidOCR()

    def process(self, image_path: str, document_id: str, page: int) -> OCRPage:
        result = self._engine(image_path)

        boxes = getattr(result, "boxes", None)
        texts = getattr(result, "txts", None)
        scores = getattr(result, "scores", None)

        if boxes is None:
            boxes = []
        if texts is None:
            texts = []
        if scores is None:
            scores = []

        # Compatibility fallback for tuple/list-style RapidOCR results.
        if len(texts) == 0 and isinstance(result, (tuple, list)) and len(result) >= 3:
            boxes, texts, scores = result[0] or [], result[1] or [], result[2] or []

        blocks: list[OCRBlock] = []
        for index, text in enumerate(texts):
            score = float(scores[index]) if index < len(scores) else 0.0
            box = boxes[index] if index < len(boxes) else None
            blocks.append(
                OCRBlock(
                    block_id=f"b{index + 1:04d}",
                    text=str(text),
                    bbox=normalize_bbox(box),
                    confidence=max(0.0, min(1.0, score)),
                )
            )

        return OCRPage(
            schema_version="1.0",
            document_id=document_id,
            page=page,
            engine="rapidocr",
            model=None,
            blocks=blocks,
        )
