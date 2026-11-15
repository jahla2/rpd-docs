from app.extraction.invoice import (
    extract_invoice,
    extract_line_items,
    normalize_date,
    normalize_decimal,
)


def test_normalizers():
    assert normalize_decimal("₱ 1,250.50") == "1250.50"
    assert normalize_date("2026-11-16") == "2026-11-16"


def test_extract_line_items_from_html():
    html = """
    <table>
      <tr><th>Description</th><th>Qty</th><th>Unit Price</th><th>Total</th></tr>
      <tr><td>Paper</td><td>2</td><td>50.00</td><td>100.00</td></tr>
    </table>
    """
    items, meta = extract_line_items(html)
    assert meta["header_row"] == 0
    assert items == [{
        "description": "Paper",
        "quantity": "2",
        "unit_price": "50.00",
        "line_total": "100.00",
    }]


def test_extract_invoice_core_fields():
    blocks = [
        {"block_id": "b1", "text": "ABC Trading Ltd", "bbox": [0,0,10,0,10,10,0,10], "confidence": 0.99},
        {"block_id": "b2", "text": "Invoice No: INV-001", "bbox": [0,0,10,0,10,10,0,10], "confidence": 0.98},
        {"block_id": "b3", "text": "Invoice Date: 2026-11-16", "bbox": [0,0,10,0,10,10,0,10], "confidence": 0.97},
        {"block_id": "b4", "text": "Currency PHP", "bbox": [0,0,10,0,10,10,0,10], "confidence": 0.96},
        {"block_id": "b5", "text": "Grand Total: ₱1,250.50", "bbox": [0,0,10,0,10,10,0,10], "confidence": 0.95},
    ]
    result = extract_invoice("doc-1", 1, blocks, None)
    assert result["invoice_number"]["value"] == "INV-001"
    assert result["invoice_date"]["value"] == "2026-11-16"
    assert result["currency"]["value"] == "PHP"
    assert result["grand_total"]["value"] == "1250.50"
    assert result["requires_review"] is False
