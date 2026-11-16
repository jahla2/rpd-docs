from app.tables.region import detect_table_region, translate_ocr_blocks


def box(x1, y1, x2, y2):
    return [x1, y1, x2, y1, x2, y2, x1, y2]


def test_detect_table_region_from_header_geometry():
    blocks = [
        {"block_id": "h1", "text": "Description", "bbox": box(100, 300, 500, 330), "confidence": 0.99},
        {"block_id": "h2", "text": "Qty", "bbox": box(520, 300, 590, 330), "confidence": 0.99},
        {"block_id": "h3", "text": "Unit Price", "bbox": box(610, 300, 760, 330), "confidence": 0.99},
        {"block_id": "h4", "text": "Total", "bbox": box(780, 300, 900, 330), "confidence": 0.99},
        {"block_id": "r1", "text": "Paper", "bbox": box(100, 350, 500, 380), "confidence": 0.99},
        {"block_id": "r2", "text": "2", "bbox": box(520, 350, 590, 380), "confidence": 0.99},
        {"block_id": "r3", "text": "100.00", "bbox": box(780, 350, 900, 380), "confidence": 0.99},
        {"block_id": "f1", "text": "Grand Total 100.00", "bbox": box(650, 500, 900, 530), "confidence": 0.99},
    ]
    region = detect_table_region(blocks, 1000, 1400)
    assert region is not None
    assert region[1] < 300
    assert region[3] < 500


def test_translate_blocks_filters_and_rebases():
    blocks = [
        {"block_id": "a", "text": "Qty", "bbox": box(100, 200, 150, 230), "confidence": 0.9},
        {"block_id": "b", "text": "Outside", "bbox": box(900, 900, 950, 930), "confidence": 0.9},
    ]
    translated = translate_ocr_blocks(blocks, [50, 150, 500, 600])
    assert len(translated) == 1
    assert translated[0]["bbox"][0] == 50.0
    assert translated[0]["bbox"][1] == 50.0


def test_detect_statement_table_region_with_schema_headers():
    blocks = [
        {"block_id": "h1", "text": "Date", "bbox": box(80, 280, 160, 310), "confidence": 0.99},
        {"block_id": "h2", "text": "Invoice No", "bbox": box(200, 280, 340, 310), "confidence": 0.99},
        {"block_id": "h3", "text": "Debit", "bbox": box(500, 280, 590, 310), "confidence": 0.99},
        {"block_id": "h4", "text": "Credit", "bbox": box(620, 280, 710, 310), "confidence": 0.99},
        {"block_id": "h5", "text": "Balance", "bbox": box(760, 280, 900, 310), "confidence": 0.99},
        {"block_id": "r1", "text": "2026-11-10", "bbox": box(80, 340, 180, 370), "confidence": 0.99},
        {"block_id": "r2", "text": "INV-9", "bbox": box(200, 340, 340, 370), "confidence": 0.99},
        {"block_id": "r3", "text": "6500.00", "bbox": box(760, 340, 900, 370), "confidence": 0.99},
    ]
    aliases = {
        "date": ["date", "transaction date"],
        "invoice_number": ["invoice no", "invoice number"],
        "debit": ["debit"],
        "credit": ["credit"],
        "balance": ["balance"],
    }
    region = detect_table_region(blocks, 1000, 1400, header_aliases=aliases)
    assert region is not None
    assert region[1] < 280
    assert region[2] >= 900
