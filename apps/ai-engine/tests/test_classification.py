from app.classification.rules import classify_document


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


def test_invoice_is_classified_by_fast_rules() -> None:
    result = classify_document(
        document_id="doc-1",
        pages=[_page("ABC Trading", "Invoice Number: INV-001", "Invoice Date: 2026-11-16", "Subtotal: 1000.00", "VAT: 120.00", "Grand Total: 1120.00")],
    )
    assert result["document_type"] == "invoice"
    assert result["confidence"] >= 0.90
    assert result["requires_review"] is False


def test_purchase_order_is_classified() -> None:
    result = classify_document(
        document_id="doc-2",
        pages=[_page("PURCHASE ORDER", "PO Number: PO-0091", "Order Date: 2026-11-16", "Ship To: Main Warehouse")],
    )
    assert result["document_type"] == "purchase_order"
    assert result["confidence"] >= 0.90


def test_ambiguous_document_becomes_unknown() -> None:
    result = classify_document(
        document_id="doc-3",
        pages=[_page("ABC Trading", "Reference 12345", "Total 100.00")],
    )
    assert result["document_type"] == "unknown"
    assert result["requires_review"] is True


def test_explicit_hint_has_priority() -> None:
    result = classify_document(
        document_id="doc-4",
        pages=[_page("Invoice Number: INV-001", "Grand Total 10.00")],
        document_type_hint="credit_note",
        hint_source="explicit",
    )
    assert result["document_type"] == "credit_note"
    assert result["confidence"] == 1.0
    assert result["method"] == "explicit"
