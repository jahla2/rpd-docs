from app.ocr.rapidocr_adapter import normalize_bbox


def test_normalize_polygon_bbox():
    assert normalize_bbox([[1, 2], [3, 4], [5, 6], [7, 8]]) == [
        1.0, 2.0, 3.0, 4.0, 5.0, 6.0, 7.0, 8.0
    ]


def test_normalize_empty_bbox():
    assert normalize_bbox(None) == [0.0, 0.0, 0.0, 0.0]
