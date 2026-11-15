from app.extraction.invoice import extract_invoice_document


def box():
    return [0, 0, 100, 0, 100, 20, 0, 20]


def test_custom_document_field_aliases():
    config = {
        "columns": [
            {"source_aliases": ["Vendor Name"], "target": "supplier_name", "type": "string"},
            {"source_aliases": ["Document Ref"], "target": "invoice_number", "type": "string"},
            {"source_aliases": ["Posting Dt"], "target": "invoice_date", "type": "date", "operations": [{"operation": "date"}]},
            {"source_aliases": ["CCY"], "target": "currency", "type": "currency", "operations": [{"operation": "normalize", "config": {"case": "upper"}}]},
            {"source_aliases": ["Amount Payable"], "target": "grand_total", "type": "decimal", "operations": [{"operation": "decimal"}]},
        ]
    }
    blocks = [
        {"block_id": "b1", "text": "Vendor Name: ACME LTD", "bbox": box(), "confidence": 1.0},
        {"block_id": "b2", "text": "Document Ref: DOC-77", "bbox": box(), "confidence": 1.0},
        {"block_id": "b3", "text": "Posting Dt: 2026-11-16", "bbox": box(), "confidence": 1.0},
        {"block_id": "b4", "text": "CCY: php", "bbox": box(), "confidence": 1.0},
        {"block_id": "b5", "text": "Amount Payable: 1,500.25", "bbox": box(), "confidence": 1.0},
    ]
    result = extract_invoice_document(
        "doc-77",
        [{"page": 1, "ocr_blocks": blocks, "table_html": None}],
        config,
    )
    assert result["supplier_name"]["value"] == "ACME LTD"
    assert result["invoice_number"]["value"] == "DOC-77"
    assert result["invoice_date"]["value"] == "2026-11-16"
    assert result["currency"]["value"] == "PHP"
    assert result["grand_total"]["value"] == "1500.25"
    assert result["requires_review"] is False
