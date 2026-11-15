from app.extraction.invoice import extract_invoice_document


def box():
    return [0, 0, 10, 0, 10, 10, 0, 10]


def test_merge_invoice_across_pages():
    pages = [
        {
            "page": 1,
            "ocr_blocks": [
                {"block_id": "p1-1", "text": "ABC Trading", "bbox": box(), "confidence": 0.99},
                {"block_id": "p1-2", "text": "Invoice No: INV-900", "bbox": box(), "confidence": 0.98},
                {"block_id": "p1-3", "text": "Invoice Date: 2026-11-16", "bbox": box(), "confidence": 0.97},
                {"block_id": "p1-4", "text": "Currency PHP", "bbox": box(), "confidence": 0.96},
            ],
            "table_html": "<table><tr><th>Description</th><th>Qty</th><th>Total</th></tr><tr><td>Paper</td><td>2</td><td>100.00</td></tr></table>",
        },
        {
            "page": 2,
            "ocr_blocks": [
                {"block_id": "p2-1", "text": "Grand Total: PHP 100.00", "bbox": box(), "confidence": 1.0},
            ],
            "table_html": None,
        },
    ]

    result = extract_invoice_document("doc-900", pages)
    assert result["page_count"] == 2
    assert result["invoice_number"]["value"] == "INV-900"
    assert result["grand_total"]["value"] == "100.00"
    assert result["grand_total"]["evidence"]["page"] == 2
    assert len(result["line_items"]) == 1
    assert result["requires_review"] is False
