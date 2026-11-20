from __future__ import annotations

import json
import os
import re
from dataclasses import dataclass
from typing import Any

import httpx

from app.extraction.invoice import normalize_date, normalize_decimal, parse_table_html


SUPPLIER_STATEMENT_FIELDS: dict[str, str] = {
    "supplier_name": "string",
    "account_number": "identifier",
    "statement_date": "date",
    "currency": "currency",
    "opening_balance": "decimal",
    "closing_balance": "decimal",
}


@dataclass(slots=True)
class DocumentReviewProposal:
    provider: str
    model: str
    prompt_version: str
    confidence: float
    field_updates: dict[str, str]
    field_removals: list[str]
    evidence: dict[str, list[str]]
    suggestions: list[str]
    summary: str
    raw_response: dict[str, Any]


def _system_prompt(document_type: str) -> str:
    return f"""You are a local financial-document review agent.
The document type is {document_type}.

Your job is to review deterministic extraction problems, inspect only the supplied evidence through tools,
and propose a conservative patch. Never invent values. Never approve payments, reconciliation, accounting
entries, or financial decisions. Never request shell, database, network, filesystem, or write access.

You must use the available read-only tools before proposing a patch.
Finish by calling propose_document_patch exactly once.

For supplier_statement, only these top-level fields may be changed:
supplier_name, account_number, statement_date, currency, opening_balance, closing_balance.

Rules:
- Every field update must be supported by OCR block IDs supplied in the evidence object.
- If a current field is clearly a false positive, you may propose removing it.
- Do not rewrite transaction rows in v1. Instead put transaction/schema concerns in suggestions.
- Prefer no change over an unsupported guess.
- Keep the summary concise and evidence-based.
"""


def _block_lookup(pages: list[dict[str, Any]]) -> dict[str, dict[str, Any]]:
    lookup: dict[str, dict[str, Any]] = {}
    for page in pages[:20]:
        page_number = int(page.get("page") or 0)
        for block in (page.get("ocr_blocks") or [])[:400]:
            block_id = str(block.get("block_id") or "").strip()
            if not block_id:
                continue
            lookup[block_id] = {
                "block_id": block_id,
                "page": page_number,
                "text": str(block.get("text") or ""),
                "confidence": float(block.get("confidence") or 0.0),
                "bbox": list(block.get("bbox") or []),
            }
    return lookup


def _normalize_evidence_text(value: str) -> str:
    return " ".join(re.sub(r"[^a-z0-9.]+", " ", value.lower()).split())


def _value_supported(value: str, data_type: str, blocks: list[dict[str, Any]]) -> bool:
    if not blocks:
        return False

    texts = [str(block.get("text") or "").strip() for block in blocks]
    combined = " ".join(texts)
    normalized_combined = _normalize_evidence_text(combined)
    normalized_value = _normalize_evidence_text(value)

    if data_type == "date":
        for text in texts:
            normalized = normalize_date(text)
            if normalized and normalized == value:
                return True
        return False

    if data_type == "decimal":
        for text in texts:
            normalized = normalize_decimal(text)
            if normalized and normalized == value:
                return True
            for token in re.findall(r"[-+]?\d[\d,]*(?:\.\d+)?", text):
                normalized = normalize_decimal(token)
                if normalized and normalized == value:
                    return True
        return False

    if data_type == "currency":
        return value.upper() in combined.upper()

    return bool(normalized_value) and normalized_value in normalized_combined


def _sanitize_proposal(
    payload: dict[str, Any],
    *,
    provider: str,
    model: str,
    document_type: str,
    pages: list[dict[str, Any]],
) -> DocumentReviewProposal:
    if document_type != "supplier_statement":
        raise RuntimeError(f"document review is not enabled for {document_type!r}")

    lookup = _block_lookup(pages)
    raw_evidence = payload.get("evidence") or {}
    evidence: dict[str, list[str]] = {}
    if isinstance(raw_evidence, dict):
        for field, ids in raw_evidence.items():
            if field not in SUPPLIER_STATEMENT_FIELDS or not isinstance(ids, list):
                continue
            cleaned = [str(value) for value in ids[:20] if str(value) in lookup]
            if cleaned:
                evidence[field] = cleaned

    updates: dict[str, str] = {}
    raw_updates = payload.get("field_updates") or {}
    if isinstance(raw_updates, dict):
        for field, raw_value in raw_updates.items():
            if field not in SUPPLIER_STATEMENT_FIELDS:
                continue
            if not isinstance(raw_value, (str, int, float)):
                continue
            value = str(raw_value).strip()
            if not value:
                continue

            data_type = SUPPLIER_STATEMENT_FIELDS[field]
            if data_type == "date":
                value = normalize_date(value) or value
            elif data_type == "decimal":
                value = normalize_decimal(value) or value
            elif data_type == "currency":
                value = value.upper()[:3]

            support = [lookup[block_id] for block_id in evidence.get(field, []) if block_id in lookup]
            if _value_supported(value, data_type, support):
                updates[field] = value

    removals: list[str] = []
    raw_removals = payload.get("field_removals") or []
    if isinstance(raw_removals, list):
        for value in raw_removals[:20]:
            field = str(value).strip()
            if field in SUPPLIER_STATEMENT_FIELDS and field not in removals:
                removals.append(field)

    suggestions_raw = payload.get("suggestions") or []
    suggestions = []
    if isinstance(suggestions_raw, list):
        suggestions = [str(value).strip()[:500] for value in suggestions_raw[:10] if str(value).strip()]

    try:
        confidence = float(payload.get("confidence", 0.0))
    except (TypeError, ValueError):
        confidence = 0.0
    confidence = max(0.0, min(1.0, confidence))

    return DocumentReviewProposal(
        provider=provider,
        model=model,
        prompt_version="document-reviewer-v1",
        confidence=confidence,
        field_updates=updates,
        field_removals=removals,
        evidence=evidence,
        suggestions=suggestions,
        summary=str(payload.get("summary") or "").strip()[:1000],
        raw_response=payload,
    )


def _tools() -> list[dict[str, Any]]:
    no_args = {"type": "object", "properties": {}}
    return [
        {
            "type": "function",
            "function": {
                "name": "get_validation_issues",
                "description": "Read deterministic validation failures and warnings for the document.",
                "parameters": no_args,
            },
        },
        {
            "type": "function",
            "function": {
                "name": "get_current_canonical",
                "description": "Read the current deterministic canonical document.",
                "parameters": no_args,
            },
        },
        {
            "type": "function",
            "function": {
                "name": "search_ocr",
                "description": "Search OCR blocks for a phrase or value and return matching block IDs and nearby text.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "query": {"type": "string"},
                        "max_results": {"type": "integer", "minimum": 1, "maximum": 20},
                    },
                    "required": ["query"],
                },
            },
        },
        {
            "type": "function",
            "function": {
                "name": "get_table_rows",
                "description": "Read recognized table rows from all pages for mapping review.",
                "parameters": no_args,
            },
        },
        {
            "type": "function",
            "function": {
                "name": "propose_document_patch",
                "description": "Finish the review with an evidence-backed document patch and concise suggestions.",
                "parameters": {
                    "type": "object",
                    "properties": {
                        "confidence": {"type": "number", "minimum": 0, "maximum": 1},
                        "field_updates": {"type": "object", "additionalProperties": {"type": ["string", "number"]}},
                        "field_removals": {"type": "array", "items": {"type": "string"}},
                        "evidence": {
                            "type": "object",
                            "additionalProperties": {"type": "array", "items": {"type": "string"}},
                        },
                        "suggestions": {"type": "array", "items": {"type": "string"}},
                        "summary": {"type": "string"},
                    },
                    "required": ["confidence", "field_updates", "evidence", "summary"],
                },
            },
        },
    ]


def _execute_tool(
    name: str,
    arguments: dict[str, Any],
    *,
    canonical: dict[str, Any],
    validation: dict[str, Any],
    pages: list[dict[str, Any]],
) -> dict[str, Any]:
    if name == "get_validation_issues":
        return validation

    if name == "get_current_canonical":
        return canonical

    if name == "search_ocr":
        query = str(arguments.get("query") or "").strip().lower()
        max_results = max(1, min(20, int(arguments.get("max_results") or 8)))
        if not query:
            return {"matches": []}
        matches = []
        flat = list(_block_lookup(pages).values())
        for index, block in enumerate(flat):
            if query not in str(block["text"]).lower():
                continue
            start = max(0, index - 1)
            end = min(len(flat), index + 2)
            matches.append({
                "block_id": block["block_id"],
                "page": block["page"],
                "text": block["text"],
                "confidence": block["confidence"],
                "nearby": [
                    {"block_id": item["block_id"], "text": item["text"]}
                    for item in flat[start:end]
                ],
            })
            if len(matches) >= max_results:
                break
        return {"matches": matches}

    if name == "get_table_rows":
        pages_out = []
        for page in pages[:20]:
            rows = parse_table_html(page.get("table_html"))
            pages_out.append({
                "page": page.get("page"),
                "rows": rows[:40],
            })
        return {"pages": pages_out}

    raise RuntimeError(f"unsupported review tool {name!r}")


def _ollama_review(
    base_url: str,
    model: str,
    *,
    document_type: str,
    canonical: dict[str, Any],
    validation: dict[str, Any],
    pages: list[dict[str, Any]],
) -> dict[str, Any]:
    messages: list[dict[str, Any]] = [
        {"role": "system", "content": _system_prompt(document_type)},
        {
            "role": "user",
            "content": (
                "Review this document. Start with validation and canonical state, then search OCR/table evidence "
                "for missing or suspicious fields. Finish with propose_document_patch."
            ),
        },
    ]
    tools = _tools()
    num_ctx = int(os.getenv("AGENT_NUM_CTX", "8192"))

    for _ in range(8):
        response = httpx.post(
            base_url.rstrip("/") + "/api/chat",
            timeout=120.0,
            json={
                "model": model,
                "stream": False,
                "think": False,
                "tools": tools,
                "options": {"temperature": 0, "num_ctx": max(4096, min(32768, num_ctx))},
                "messages": messages,
            },
        )
        response.raise_for_status()
        outer = response.json()
        message = outer.get("message") or {}
        messages.append(message)

        tool_calls = message.get("tool_calls") or []
        if not tool_calls:
            raise RuntimeError("document review agent returned no tool call")

        for call in tool_calls:
            function = call.get("function") or {}
            name = str(function.get("name") or "")
            arguments = function.get("arguments") or {}
            if not isinstance(arguments, dict):
                arguments = {}

            if name == "propose_document_patch":
                return arguments

            result = _execute_tool(
                name,
                arguments,
                canonical=canonical,
                validation=validation,
                pages=pages,
            )
            messages.append({
                "role": "tool",
                "content": json.dumps(result, separators=(",", ":")),
            })

    raise RuntimeError("document review agent exceeded tool-call limit")


def review_document(
    *,
    document_type: str,
    canonical: dict[str, Any],
    validation: dict[str, Any],
    pages: list[dict[str, Any]],
) -> DocumentReviewProposal:
    provider = os.getenv("AGENT_PROVIDER", "disabled").strip().lower()
    model = os.getenv("AGENT_MODEL", "").strip()
    document_type = document_type.strip().lower()

    if provider == "disabled":
        raise RuntimeError("agent fallback provider is disabled")
    if document_type != "supplier_statement":
        raise RuntimeError(f"document review is not enabled for {document_type!r}")

    if provider != "ollama":
        raise RuntimeError("document-reviewer-v1 currently requires AGENT_PROVIDER=ollama")

    base_url = os.getenv("AGENT_BASE_URL", "http://localhost:11434").strip()
    if not model:
        model = "qwen3.5:9b"

    payload = _ollama_review(
        base_url,
        model,
        document_type=document_type,
        canonical=canonical,
        validation=validation,
        pages=pages,
    )
    return _sanitize_proposal(
        payload,
        provider=provider,
        model=model,
        document_type=document_type,
        pages=pages,
    )


def apply_document_proposal(
    canonical: dict[str, Any],
    proposal: DocumentReviewProposal,
    pages: list[dict[str, Any]],
) -> dict[str, Any]:
    result = json.loads(json.dumps(canonical))
    fields = result.setdefault("fields", {})
    lookup = _block_lookup(pages)

    for field in proposal.field_removals:
        fields.pop(field, None)

    for field, value in proposal.field_updates.items():
        block_ids = proposal.evidence.get(field, [])
        blocks = [lookup[block_id] for block_id in block_ids if block_id in lookup]
        if not blocks:
            continue
        page = int(blocks[0].get("page") or 0)
        confidence = min((float(block.get("confidence") or 0.0) for block in blocks), default=0.0)
        fields[field] = {
            "raw": " | ".join(str(block.get("text") or "") for block in blocks),
            "value": value,
            "data_type": SUPPLIER_STATEMENT_FIELDS[field],
            "evidence": {
                "page": page,
                "block_ids": block_ids,
                "bbox": [],
                "bbox_normalized": [],
                "method": "agent_review",
                "confidence": min(proposal.confidence, confidence) if confidence > 0 else proposal.confidence,
                "provider": proposal.provider,
                "model": proposal.model,
                "prompt_version": proposal.prompt_version,
            },
        }

    required = ["supplier_name", "closing_balance"]
    missing = [name for name in required if not (fields.get(name) or {}).get("value")]
    result["requires_review"] = bool(missing)
    validation_summary = result.setdefault("validation", {})
    validation_summary["missing_required_fields"] = missing
    result["agent_review"] = {
        "provider": proposal.provider,
        "model": proposal.model,
        "prompt_version": proposal.prompt_version,
        "confidence": proposal.confidence,
        "summary": proposal.summary,
        "suggestions": proposal.suggestions,
        "field_updates": proposal.field_updates,
        "field_removals": proposal.field_removals,
        "evidence": proposal.evidence,
    }
    return result
