from app.agents.document_reviewer import DocumentReviewProposal
from app.agents.glm_ocr_reviewer import (
    _attach_evidence,
    _json_payload,
    glm_ocr_useful,
)


def _pages() -> list[dict]:
    return [
        {
            "page": 1,
            "ocr_blocks": [
                {
                    "block_id": "p001-t0002",
                    "text": "Redwood Identity Services Ltd",
                    "bbox": [10, 10, 100, 20],
                    "confidence": 0.99,
                },
                {
                    "block_id": "p001-t0010",
                    "text": "01 July 2026",
                    "bbox": [10, 30, 100, 40],
                    "confidence": 0.98,
                },
                {
                    "block_id": "p001-t0043",
                    "text": "BALANCE DUE NZD",
                    "bbox": [10, 50, 100, 60],
                    "confidence": 0.99,
                },
                {
                    "block_id": "p001-t0044",
                    "text": "513.35",
                    "bbox": [110, 50, 160, 60],
                    "confidence": 0.99,
                },
            ],
        }
    ]


def test_glm_payload_is_recovered_from_markdown_fence() -> None:
    payload = _json_payload(
        '```json\n{"confidence":0.95,"field_updates":{"closing_balance":"513.35"}}\n```'
    )
    assert payload["confidence"] == 0.95
    assert payload["field_updates"]["closing_balance"] == "513.35"


def test_glm_updates_are_attached_to_existing_ocr_evidence() -> None:
    payload = _attach_evidence(
        {
            "confidence": 0.96,
            "field_updates": {
                "supplier_name": "Redwood Identity Services Ltd",
                "statement_date": "2026-07-01",
                "currency": "NZD",
                "closing_balance": "513.35",
                "account_number": "invented-account",
            },
            "field_removals": ["account_number"],
        },
        _pages(),
    )

    assert payload["evidence"]["supplier_name"] == ["p001-t0002"]
    assert payload["evidence"]["statement_date"] == ["p001-t0010"]
    assert payload["evidence"]["currency"] == ["p001-t0043"]
    assert payload["evidence"]["closing_balance"] == ["p001-t0044"]
    assert "account_number" not in payload["evidence"]


def test_glm_useful_requires_confidence_and_a_real_change(monkeypatch) -> None:
    monkeypatch.setenv("GLM_OCR_MIN_CONFIDENCE", "0.88")

    useful = DocumentReviewProposal(
        provider="ollama",
        model="glm-ocr:q8_0",
        prompt_version="glm-ocr-document-review-v1",
        confidence=0.93,
        field_updates={"closing_balance": "513.35"},
        field_removals=[],
        evidence={"closing_balance": ["p001-t0044"]},
        suggestions=[],
        summary="recovered balance",
        raw_response={},
    )
    assert glm_ocr_useful(useful) is True

    weak = DocumentReviewProposal(
        provider="ollama",
        model="glm-ocr:q8_0",
        prompt_version="glm-ocr-document-review-v1",
        confidence=0.70,
        field_updates={"closing_balance": "513.35"},
        field_removals=[],
        evidence={"closing_balance": ["p001-t0044"]},
        suggestions=[],
        summary="low confidence",
        raw_response={},
    )
    assert glm_ocr_useful(weak) is False

    no_change = DocumentReviewProposal(
        provider="ollama",
        model="glm-ocr:q8_0",
        prompt_version="glm-ocr-document-review-v1",
        confidence=0.99,
        field_updates={},
        field_removals=[],
        evidence={},
        suggestions=[],
        summary="nothing to change",
        raw_response={},
    )
    assert glm_ocr_useful(no_change) is False