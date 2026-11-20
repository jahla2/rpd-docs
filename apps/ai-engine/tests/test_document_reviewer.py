from app.agents.document_reviewer import (
    DocumentReviewProposal,
    _compact_canonical,
    _proposal_acceptable,
    _sanitize_proposal,
    apply_document_proposal,
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


def test_supplier_statement_review_applies_only_evidence_backed_values() -> None:
    pages = _pages()
    proposal = _sanitize_proposal(
        {
            "confidence": 0.97,
            "field_updates": {
                "supplier_name": "Redwood Identity Services Ltd",
                "statement_date": "2026-07-01",
                "currency": "NZD",
                "closing_balance": "513.35",
                "account_number": "invented-account",
            },
            "field_removals": ["account_number"],
            "evidence": {
                "supplier_name": ["p001-t0002"],
                "statement_date": ["p001-t0010"],
                "currency": ["p001-t0043"],
                "closing_balance": ["p001-t0044"],
                "account_number": ["p001-t0044"],
            },
            "suggestions": ["Map Activity, Reference, Due Date, Invoice Amount, Payments and Balance columns."],
            "summary": "Recovered missing statement fields from OCR evidence.",
        },
        provider="ollama",
        model="qwen3.5:9b",
        document_type="supplier_statement",
        pages=pages,
    )

    assert proposal.field_updates == {
        "supplier_name": "Redwood Identity Services Ltd",
        "statement_date": "2026-07-01",
        "currency": "NZD",
        "closing_balance": "513.35",
    }
    assert proposal.field_removals == ["account_number"]

    canonical = {
        "document_type": "supplier_statement",
        "fields": {
            "account_number": {
                "value": "HAS CHANGED:",
                "raw": "PLEASE NOTE OUR BANK ACCOUNT NUMBER HAS CHANGED:",
            }
        },
        "tables": {"transactions": [{"date": "04 Apr 2026", "credit": "0.00"}]},
        "requires_review": True,
        "validation": {"missing_required_fields": ["supplier_name", "closing_balance"]},
    }

    resolved = apply_document_proposal(canonical, proposal, pages)

    assert resolved["fields"]["supplier_name"]["value"] == "Redwood Identity Services Ltd"
    assert resolved["fields"]["statement_date"]["value"] == "2026-07-01"
    assert resolved["fields"]["currency"]["value"] == "NZD"
    assert resolved["fields"]["closing_balance"]["value"] == "513.35"
    assert "account_number" not in resolved["fields"]
    assert resolved["requires_review"] is True
    assert resolved["validation"]["transaction_mapping_incomplete"] is True
    assert resolved["validation"]["incomplete_transaction_rows"] == [1]
    assert resolved["agent_review"]["model"] == "qwen3.5:9b"


def test_supplier_statement_review_rejects_hallucinated_field_update() -> None:
    proposal = _sanitize_proposal(
        {
            "confidence": 0.99,
            "field_updates": {"supplier_name": "Imaginary Supplier Ltd"},
            "evidence": {"supplier_name": ["p001-t0002"]},
            "summary": "unsupported guess",
        },
        provider="ollama",
        model="qwen3.5:9b",
        document_type="supplier_statement",
        pages=_pages(),
    )

    assert proposal.field_updates == {}




def test_compact_canonical_drops_heavy_layout_metadata() -> None:
    compact = _compact_canonical({
        "document_type": "supplier_statement",
        "fields": {
            "supplier_name": {"value": "Redwood Identity Services Ltd", "raw": "Redwood Identity Services Ltd"}
        },
        "tables": {
            "transactions": [
                {
                    "page": 1,
                    "date": "2026-04-04",
                    "invoice_number": "RED-26310",
                    "credit": "0.00",
                    "unexpected": "drop-me",
                }
            ]
        },
        "table_metadata": [{"huge": "payload"}],
        "layout": {"huge": "payload"},
        "requires_review": True,
    })

    assert "table_metadata" not in compact
    assert "layout" not in compact
    assert compact["transactions"][0]["invoice_number"] == "RED-26310"
    assert "unexpected" not in compact["transactions"][0]


def test_proposal_acceptance_requires_required_fields(monkeypatch) -> None:
    monkeypatch.setenv("AGENT_MIN_CONFIDENCE", "0.90")
    canonical = {
        "fields": {},
    }
    partial = DocumentReviewProposal(
        provider="ollama",
        model="ministral-3:8b",
        prompt_version="document-reviewer-langgraph-v2",
        confidence=0.97,
        field_updates={"closing_balance": "513.35"},
        field_removals=[],
        evidence={"closing_balance": ["p001-t0044"]},
        suggestions=[],
        summary="partial",
        raw_response={},
    )
    assert _proposal_acceptable(partial, canonical=canonical) is False

    complete = DocumentReviewProposal(
        provider="ollama",
        model="ministral-3:8b",
        prompt_version="document-reviewer-langgraph-v2",
        confidence=0.97,
        field_updates={
            "supplier_name": "Redwood Identity Services Ltd",
            "closing_balance": "513.35",
        },
        field_removals=[],
        evidence={
            "supplier_name": ["p001-t0002"],
            "closing_balance": ["p001-t0044"],
        },
        suggestions=[],
        summary="complete",
        raw_response={},
    )
    assert _proposal_acceptable(complete, canonical=canonical) is True
