from app.agents.invoice_resolver import AgentProposal, apply_proposal, _sanitize_proposal


def test_sanitize_rejects_unknown_fields():
    proposal = _sanitize_proposal(
        {
            "confidence": 1.5,
            "field_updates": {
                "invoice_number": "INV-5",
                "approve_payment": "yes",
            },
            "evidence_block_ids": ["b1"],
            "summary": "Invoice number is visible.",
        },
        "test",
        "model",
    )
    assert proposal.confidence == 1.0
    assert proposal.field_updates == {"invoice_number": "INV-5"}


def test_apply_proposal_marks_agent_evidence():
    proposal = AgentProposal(
        provider="test",
        model="model",
        prompt_version="invoice-resolver-v1",
        confidence=0.95,
        field_updates={"invoice_number": "INV-99"},
        evidence_block_ids=["b7"],
        summary="Supported by OCR.",
        raw_response={},
    )
    result = apply_proposal({"invoice_number": None}, proposal)
    assert result["invoice_number"]["value"] == "INV-99"
    assert result["invoice_number"]["evidence"]["method"] == "agent_fallback"
