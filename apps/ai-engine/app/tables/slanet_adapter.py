from __future__ import annotations

from dataclasses import dataclass
from typing import Any


@dataclass(slots=True)
class TableResult:
    schema_version: str
    model: str
    pred_html: str | None
    cell_bboxes: list
    logic_points: list
    elapsed: float | None


def _to_list(value: Any) -> list:
    if value is None:
        return []
    if hasattr(value, "tolist"):
        return value.tolist()
    if isinstance(value, list):
        return value
    return list(value)


def _first_result(result: Any, plural: str, singular: str) -> Any:
    plural_value = getattr(result, plural, None)
    if plural_value is not None:
        try:
            if len(plural_value) > 0:
                return plural_value[0]
        except TypeError:
            pass
    return getattr(result, singular, None)


def _bbox_to_quad(bbox: list[float]) -> list[list[float]]:
    if len(bbox) >= 8:
        return [
            [float(bbox[0]), float(bbox[1])],
            [float(bbox[2]), float(bbox[3])],
            [float(bbox[4]), float(bbox[5])],
            [float(bbox[6]), float(bbox[7])],
        ]
    if len(bbox) == 4:
        x1, y1, x2, y2 = (float(v) for v in bbox)
        return [[x1, y1], [x2, y1], [x2, y2], [x1, y2]]
    raise ValueError("OCR bbox must contain 4 or 8 numeric coordinates")


class SLANetTableEngine:
    """RapidTable SLANet+ adapter for a single page/table image.

    RapidTable v3 returns list-based batch fields. This adapter also tolerates
    the v2 singular fields so stored workers can roll forward safely.
    """

    def __init__(self) -> None:
        try:
            from rapid_table import ModelType, RapidTable, RapidTableInput
        except ImportError as exc:
            raise RuntimeError(
                "RapidTable is not installed. Run: pip install -e '.[ocr]'"
            ) from exc

        self._engine = RapidTable(
            RapidTableInput(model_type=ModelType.SLANETPLUS)
        )

    def process(self, image_path: str, ocr_blocks: list[dict] | None = None) -> TableResult:
        ocr_results = None

        if ocr_blocks:
            try:
                import numpy as np
            except ImportError as exc:
                raise RuntimeError("numpy is required for OCR-box reuse") from exc

            boxes = np.asarray(
                [_bbox_to_quad(block["bbox"]) for block in ocr_blocks],
                dtype=np.float32,
            )
            texts = tuple(str(block.get("text", "")) for block in ocr_blocks)
            scores = tuple(float(block.get("confidence", 0.0)) for block in ocr_blocks)
            ocr_results = [(boxes, texts, scores)]

        if ocr_results is None:
            result = self._engine(image_path)
        else:
            result = self._engine(image_path, ocr_results=ocr_results)

        pred_html = _first_result(result, "pred_htmls", "pred_html")
        cell_bboxes = _first_result(result, "cell_bboxes", "cell_bboxes")
        logic_points = _first_result(result, "logic_points", "logic_points")
        elapsed = getattr(result, "elapse", None)

        # In v3 cell_bboxes/logic_points are batch lists. The helper above is
        # only safe for fields whose singular name differs, so explicitly
        # unwrap list-based arrays when necessary.
        raw_cells = getattr(result, "cell_bboxes", None)
        if isinstance(raw_cells, list) and raw_cells:
            cell_bboxes = raw_cells[0]
        raw_logic = getattr(result, "logic_points", None)
        if isinstance(raw_logic, list) and raw_logic:
            logic_points = raw_logic[0]

        return TableResult(
            schema_version="1.0",
            model="slanet_plus",
            pred_html=str(pred_html) if pred_html is not None else None,
            cell_bboxes=_to_list(cell_bboxes),
            logic_points=_to_list(logic_points),
            elapsed=float(elapsed) if elapsed is not None else None,
        )
