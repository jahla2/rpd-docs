from __future__ import annotations


class SLANetTableEngine:
    """Lazy RapidTable/SLANet adapter.

    Table-region detection and OCR-box reuse are intentionally the next
    vertical slice; this establishes the model boundary without loading it
    during service startup.
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

    def process(self, image_path: str):
        return self._engine(image_path)
