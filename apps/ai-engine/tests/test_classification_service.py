from __future__ import annotations

from app.classification.service import ClassificationService


class FakeModel:
    def __init__(self, document_type: str = "supplier_statement", confidence: float = 0.91) -> None:
        self.calls = 0
        self.document_type = document_type
        self.confidence = confidence

    def health(self) -> dict:
        return {"ready": True, "provider": "fake"}

    def classify(self, text: str) -> dict:
        self.calls += 1
        return {
            "document_type": self.document_type,
            "confidence": self.confidence,
            "alternatives": [
                {"type": self.document_type, "confidence": self.confidence},
                {"type": "invoice", "confidence": max(0.0, 1.0 - self.confidence)},
            ],
            "latency_ms": 3,
            "model": "fake-modernbert",
            "model_version": "test",
        }


def _page(*texts: str) -> dict:
    return {
        "page": 1,
        "ocr_blocks": [
            {
                "block_id": f"b{index:04d}",
                "text": text,
                "bbox": [0, 0, 10, 10],
                "confidence": 0.99,
            }
            for index, text in enumerate(texts, start=1)
        ],
    }


def test_high_confidence_rules_skip_modernbert() -> None:
    model = FakeModel()
    service = ClassificationService(backend="modernbert", model=model)

    result = service.classify(
        document_id="doc-fast",
        pages=[_page("TAX INVOICE", "Invoice Number INV-9", "Invoice Date 2026-11-16", "Grand Total 10.00")],
    )

    assert result["document_type"] == "invoice"
    assert result["method"] == "deterministic_rules"
    assert model.calls == 0


def test_ambiguous_rules_use_modernbert() -> None:
    model = FakeModel("supplier_statement", 0.91)
    service = ClassificationService(backend="modernbert", model=model)

    result = service.classify(
        document_id="doc-fallback",
        pages=[_page("ABC Trading", "Previous activity", "Balance 123.00")],
    )

    assert result["document_type"] == "supplier_statement"
    assert result["method"] == "modernbert_zeroshot"
    assert result["requires_review"] is False
    assert model.calls == 1


def test_low_confidence_modernbert_stays_unknown() -> None:
    model = FakeModel("receipt", 0.55)
    service = ClassificationService(backend="modernbert", model=model)

    result = service.classify(
        document_id="doc-uncertain",
        pages=[_page("ABC Trading", "Reference 123", "Total 10.00")],
    )

    assert result["document_type"] == "unknown"
    assert result["requires_review"] is True
    assert model.calls == 1


def test_explicit_hint_skips_every_fallback() -> None:
    model = FakeModel()
    service = ClassificationService(backend="modernbert", model=model)

    result = service.classify(
        document_id="doc-hint",
        pages=[_page("Invoice Number INV-1")],
        document_type_hint="credit_note",
        hint_source="explicit",
    )

    assert result["document_type"] == "credit_note"
    assert result["method"] == "explicit"
    assert model.calls == 0
