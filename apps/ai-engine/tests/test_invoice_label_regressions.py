from app.extraction.invoice import extract_invoice


def box():
    return [0, 0, 10, 0, 10, 10, 0, 10]


def test_subtotal_is_not_used_as_grand_total():
    blocks = [
        {"block_id": "b1", "text": "ABC Trading", "bbox": box(), "confidence": 1.0},
        {"block_id": "b2", "text": "Invoice No: INV-1", "bbox": box(), "confidence": 1.0},
        {"block_id": "b3", "text": "Invoice Date: 2026-11-16", "bbox": box(), "confidence": 1.0},
        {"block_id": "b4", "text": "PHP", "bbox": box(), "confidence": 1.0},
        {"block_id": "b5", "text": "Subtotal: 100.00", "bbox": box(), "confidence": 1.0},
        {"block_id": "b6", "text": "Grand Total: 112.00", "bbox": box(), "confidence": 1.0},
    ]
    result = extract_invoice("doc", 1, blocks, None)
    assert result["subtotal"]["value"] == "100.00"
    assert result["grand_total"]["value"] == "112.00"


def test_due_date_is_not_used_as_invoice_date():
    blocks = [
        {"block_id": "b1", "text": "Due Date: 2026-12-01", "bbox": box(), "confidence": 1.0},
        {"block_id": "b2", "text": "Invoice Date: 2026-11-16", "bbox": box(), "confidence": 1.0},
    ]
    result = extract_invoice("doc", 1, blocks, None)
    assert result["invoice_date"]["value"] == "2026-11-16"
    assert result["due_date"]["value"] == "2026-12-01"
