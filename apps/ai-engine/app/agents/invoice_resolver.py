from __future__ import annotations

import json
import os
from dataclasses import dataclass
from typing import Any

import httpx


ALLOWED_FIELDS = {
    "supplier_name",
    "supplier_code",
    "invoice_number",
    "invoice_date",
    "due_date",
    "po_number",
    "currency",
    "subtotal",
    "tax",
    "grand_total",
}


@dataclass(slots=True)
class AgentProposal:
    provider: str
    model: str
    prompt_version: str
    confidence: float
    field_updates: dict[str, str]
    evidence_block_ids: list[str]
    summary: str
    raw_response: dict[str, Any]


def _system_prompt() -> str:
    return """You resolve uncertain invoice fields from OCR evidence.
Return JSON only. Never approve payments, reconciliation, accounting entries, or financial decisions.
Only propose corrections for these invoice fields:
supplier_name, supplier_code, invoice_number, invoice_date, due_date, po_number,
currency, subtotal, tax, grand_total.

Use only values supported by supplied OCR blocks. Do not invent missing values.
Return:
{
  "confidence": 0.0-1.0,
  "field_updates": {"field": "value"},
  "evidence_block_ids": ["block-id"],
  "summary": "brief evidence-based explanation"
}
The summary must be concise and must not contain hidden chain-of-thought.
"""


def _sanitize_proposal(payload: dict[str, Any], provider: str, model: str) -> AgentProposal:
    updates: dict[str, str] = {}
    raw_updates = payload.get("field_updates") or {}
    if isinstance(raw_updates, dict):
        for key, value in raw_updates.items():
            if key in ALLOWED_FIELDS and isinstance(value, (str, int, float)):
                normalized = str(value).strip()
                if normalized:
                    updates[key] = normalized

    block_ids = payload.get("evidence_block_ids") or []
    if not isinstance(block_ids, list):
        block_ids = []
    block_ids = [str(value) for value in block_ids[:50] if str(value).strip()]

    try:
        confidence = float(payload.get("confidence", 0.0))
    except (TypeError, ValueError):
        confidence = 0.0
    confidence = max(0.0, min(1.0, confidence))

    summary = str(payload.get("summary", "")).strip()[:800]

    return AgentProposal(
        provider=provider,
        model=model,
        prompt_version="invoice-resolver-v1",
        confidence=confidence,
        field_updates=updates,
        evidence_block_ids=block_ids,
        summary=summary,
        raw_response=payload,
    )


def _ollama_request(base_url: str, model: str, user_payload: dict[str, Any]) -> dict[str, Any]:
    response = httpx.post(
        base_url.rstrip("/") + "/api/chat",
        timeout=90.0,
        json={
            "model": model,
            "stream": False,
            "format": "json",
            "messages": [
                {"role": "system", "content": _system_prompt()},
                {"role": "user", "content": json.dumps(user_payload, separators=(",", ":"))},
            ],
        },
    )
    response.raise_for_status()
    outer = response.json()
    content = ((outer.get("message") or {}).get("content") or "").strip()
    return json.loads(content)


def _openai_compatible_request(base_url: str, api_key: str, model: str, user_payload: dict[str, Any]) -> dict[str, Any]:
    headers = {"Content-Type": "application/json"}
    if api_key:
        headers["Authorization"] = f"Bearer {api_key}"
    response = httpx.post(
        base_url.rstrip("/") + "/v1/chat/completions",
        timeout=90.0,
        headers=headers,
        json={
            "model": model,
            "temperature": 0,
            "response_format": {"type": "json_object"},
            "messages": [
                {"role": "system", "content": _system_prompt()},
                {"role": "user", "content": json.dumps(user_payload, separators=(",", ":"))},
            ],
        },
    )
    response.raise_for_status()
    outer = response.json()
    content = outer["choices"][0]["message"]["content"]
    return json.loads(content)


def resolve_invoice(
    canonical: dict[str, Any],
    validation: dict[str, Any],
    pages: list[dict[str, Any]],
    trigger_code: str,
) -> AgentProposal:
    provider = os.getenv("AGENT_PROVIDER", "disabled").strip().lower()
    model = os.getenv("AGENT_MODEL", "").strip()

    if provider == "disabled":
        raise RuntimeError("agent fallback provider is disabled")

    compact_pages = []
    for page in pages[:20]:
        compact_pages.append({
            "page": page.get("page"),
            "ocr_blocks": [
                {
                    "block_id": block.get("block_id"),
                    "text": block.get("text"),
                    "confidence": block.get("confidence"),
                }
                for block in (page.get("ocr_blocks") or [])[:300]
            ],
        })

    user_payload = {
        "trigger_code": trigger_code,
        "canonical_invoice": canonical,
        "validation": validation,
        "pages": compact_pages,
    }

    if provider == "ollama":
        base_url = os.getenv("AGENT_BASE_URL", "http://localhost:11434")
        if not model:
            model = "llama3.2"
        payload = _ollama_request(base_url, model, user_payload)
    elif provider == "openai_compatible":
        base_url = os.getenv("AGENT_BASE_URL", "").strip()
        if not base_url:
            raise RuntimeError("AGENT_BASE_URL is required for openai_compatible provider")
        if not model:
            raise RuntimeError("AGENT_MODEL is required for openai_compatible provider")
        payload = _openai_compatible_request(
            base_url,
            os.getenv("AGENT_API_KEY", ""),
            model,
            user_payload,
        )
    else:
        raise RuntimeError(f"unsupported AGENT_PROVIDER: {provider}")

    return _sanitize_proposal(payload, provider, model)


def apply_proposal(
    canonical: dict[str, Any],
    proposal: AgentProposal,
) -> dict[str, Any]:
    result = json.loads(json.dumps(canonical))
    for field, value in proposal.field_updates.items():
        result[field] = {
            "raw": value,
            "value": value,
            "evidence": {
                "method": "agent_fallback",
                "confidence": proposal.confidence,
                "block_ids": proposal.evidence_block_ids,
                "provider": proposal.provider,
                "model": proposal.model,
                "prompt_version": proposal.prompt_version,
            },
        }
    result["agent_fallback"] = {
        "provider": proposal.provider,
        "model": proposal.model,
        "prompt_version": proposal.prompt_version,
        "confidence": proposal.confidence,
        "summary": proposal.summary,
        "evidence_block_ids": proposal.evidence_block_ids,
    }
    return result
