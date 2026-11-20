from app.layout.profile import (
    build_layout_fingerprint,
    match_layout_profile,
    normalized_bbox,
)


def box(x1, y1, x2, y2):
    return [x1, y1, x2, y1, x2, y2, x1, y2]


FIELDS = {
    "supplier_name": ["supplier", "vendor"],
    "statement_date": ["statement date"],
    "closing_balance": ["closing balance", "current balance"],
}
TABLES = {
    "date": ["transaction date"],
    "invoice_number": ["invoice no"],
    "balance": ["balance"],
}


def test_normalized_bbox_is_resolution_independent():
    assert normalized_bbox(box(100, 200, 300, 400), 1000, 2000) == [0.1, 0.1, 0.3, 0.2]


def test_layout_profile_matches_same_layout_at_different_resolution():
    page_a = {
        "page": 1,
        "image_width": 1000,
        "image_height": 1400,
        "ocr_blocks": [
            {"text": "Supplier: ABC Trading", "bbox": box(100, 100, 350, 130)},
            {"text": "Statement Date: 2026-11-30", "bbox": box(650, 100, 900, 130)},
            {"text": "Current Balance: 6500.00", "bbox": box(650, 180, 900, 210)},
            {"text": "Date", "bbox": box(100, 400, 180, 430)},
            {"text": "Invoice No", "bbox": box(300, 400, 430, 430)},
            {"text": "Balance", "bbox": box(780, 400, 900, 430)},
        ],
    }
    page_b = {
        "page": 1,
        "image_width": 2000,
        "image_height": 2800,
        "ocr_blocks": [
            {"text": "Supplier: ABC Trading", "bbox": box(200, 200, 700, 260)},
            {"text": "Statement Date: 2026-12-31", "bbox": box(1300, 200, 1800, 260)},
            {"text": "Current Balance: 7100.00", "bbox": box(1300, 360, 1800, 420)},
            {"text": "Date", "bbox": box(200, 800, 360, 860)},
            {"text": "Invoice No", "bbox": box(600, 800, 860, 860)},
            {"text": "Balance", "bbox": box(1560, 800, 1800, 860)},
        ],
    }

    known = build_layout_fingerprint("supplier_statement", [page_a], FIELDS, TABLES)
    current = build_layout_fingerprint("supplier_statement", [page_b], FIELDS, TABLES)
    match = match_layout_profile(current, [{
        "id": "profile-a",
        "name": "ABC statement layout A",
        "fingerprint": known,
        "config": {"field_regions": {"closing_balance": {"bbox_normalized": [0.65, 0.12, 0.9, 0.16]}}},
    }])

    assert match["matched"] is True
    assert match["matched_profile_id"] == "profile-a"
    assert match["score"] >= 0.90


def test_layout_profile_rejects_different_layout():
    known = build_layout_fingerprint(
        "supplier_statement",
        [{
            "page": 1,
            "image_width": 1000,
            "image_height": 1400,
            "ocr_blocks": [
                {"text": "Supplier: ABC", "bbox": box(100, 100, 300, 130)},
                {"text": "Current Balance: 10.00", "bbox": box(650, 180, 900, 210)},
                {"text": "Date", "bbox": box(100, 400, 180, 430)},
                {"text": "Balance", "bbox": box(780, 400, 900, 430)},
            ],
        }],
        FIELDS,
        TABLES,
    )
    current = build_layout_fingerprint(
        "supplier_statement",
        [{
            "page": 1,
            "image_width": 1400,
            "image_height": 1000,
            "ocr_blocks": [
                {"text": "Supplier: XYZ", "bbox": box(950, 700, 1250, 740)},
                {"text": "Current Balance: 20.00", "bbox": box(100, 700, 350, 740)},
            ],
        }],
        FIELDS,
        TABLES,
    )

    match = match_layout_profile(current, [{"id": "a", "fingerprint": known, "config": {}}])
    assert match["matched"] is False
