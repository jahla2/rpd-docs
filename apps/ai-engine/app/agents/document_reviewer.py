from __future__ import annotations

import json
import os
import re
from dataclasses import dataclass
from typing import Annotated, Any, Literal

from langchain_core.messages import AIMessage, BaseMessage, HumanMessage, SystemMessage, ToolMessage
from langchain_core.tools import tool
from langchain_ollama import ChatOllama
from langgraph.graph import END, START, StateGraph, add_messages
from langgraph.prebuilt import ToolNode
from pydantic import BaseModel, Field
from typing_extensions import TypedDict

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

Your role is to investigate deterministic extraction problems with read-only tools, reason over the returned
OCR/table evidence, and identify conservative corrections. Never invent values. Never approve payments,
reconciliation, accounting entries, or financial decisions. Never request shell, database, network, filesystem,
or write access.

Use tools to inspect the deterministic validation state, current canonical values, OCR blocks, OCR search results,
and recognized table rows. Do not attempt to submit a patch as a tool call. When you have enough evidence, stop
calling tools and summarize the evidence you found; a separate structured-output node will create the patch.

For supplier_statement, only these top-level fields may be changed:
supplier_name, account_number, statement_date, currency, opening_balance, closing_balance.

Rules:
- Investigate missing required fields first.
- Investigate suspicious current values such as warning text being mistaken for an account number.
- Distinguish the statement date from transaction dates using labels and nearby OCR evidence.
- Every proposed value must be visibly supported by OCR block IDs returned by tools.
- Do not rewrite transaction rows in v1; identify transaction/schema concerns only.
- Prefer no change over an unsupported guess.
- Keep reasoning concise and evidence-focused.
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


class _DocumentPatchSchema(BaseModel):
    confidence: float = Field(default=0.0, ge=0.0, le=1.0)
    field_updates: dict[str, str] = Field(default_factory=dict)
    field_removals: list[str] = Field(default_factory=list)
    evidence: dict[str, list[str]] = Field(default_factory=dict)
    suggestions: list[str] = Field(default_factory=list)
    summary: str = ""


class _ReviewGraphState(TypedDict):
    messages: Annotated[list[BaseMessage], add_messages]
    model_turns: int
    final_payload: dict[str, Any] | None
    error: str | None


def _compact_canonical(canonical: dict[str, Any]) -> dict[str, Any]:
    fields: dict[str, Any] = {}
    for name, raw_field in (canonical.get("fields") or {}).items():
        if not isinstance(raw_field, dict):
            continue
        fields[str(name)] = {
            "value": raw_field.get("value"),
            "raw": raw_field.get("raw"),
        }

    transactions = []
    for row in ((canonical.get("tables") or {}).get("transactions") or [])[:12]:
        if isinstance(row, dict):
            transactions.append({
                str(key): value
                for key, value in row.items()
                if key in {"page", "date", "invoice_number", "reference", "due_date", "description", "debit", "credit", "balance"}
            })

    return {
        "document_type": canonical.get("document_type"),
        "fields": fields,
        "transactions": transactions,
        "requires_review": bool(canonical.get("requires_review")),
    }


def _compact_validation(validation: dict[str, Any]) -> dict[str, Any]:
    rules = []
    for rule in validation.get("rules") or []:
        if not isinstance(rule, dict):
            continue
        status = str(rule.get("status") or "").upper()
        if status not in {"FAIL", "WARN"}:
            continue
        rules.append({
            "code": rule.get("code"),
            "status": status,
            "message": rule.get("message"),
        })
        if len(rules) >= 16:
            break

    return {
        "missing_required_fields": list(validation.get("missing_required_fields") or [])[:20],
        "low_confidence_fields": list(validation.get("low_confidence_fields") or [])[:20],
        "transaction_count": validation.get("transaction_count"),
        "rules": rules,
    }


def _make_review_tools(
    *,
    canonical: dict[str, Any],
    validation: dict[str, Any],
    pages: list[dict[str, Any]],
) -> list[Any]:
    block_lookup = _block_lookup(pages)
    flat_blocks = list(block_lookup.values())

    @tool
    def get_validation_issues() -> dict[str, Any]:
        """Read deterministic validation failures and warnings for this document."""
        return _compact_validation(validation)

    @tool
    def get_current_canonical() -> dict[str, Any]:
        """Read the compact current deterministic canonical document."""
        return _compact_canonical(canonical)

    @tool
    def get_ocr_blocks(page: int, limit: int = 80) -> dict[str, Any]:
        """Read OCR blocks for one page. Use this when a field cannot be found with a targeted OCR search."""
        page_number = max(1, int(page))
        bounded_limit = max(1, min(120, int(limit)))
        blocks = [
            block for block in flat_blocks
            if int(block.get("page") or 0) == page_number
        ][:bounded_limit]
        return {
            "page": page_number,
            "blocks": [
                {
                    "block_id": block["block_id"],
                    "text": block["text"],
                    "confidence": block["confidence"],
                }
                for block in blocks
            ],
        }

    @tool
    def search_ocr(query: str, max_results: int = 8) -> dict[str, Any]:
        """Search OCR text for a phrase/value and return matching block IDs with nearby OCR context."""
        normalized_query = _normalize_evidence_text(str(query))
        bounded_results = max(1, min(12, int(max_results)))
        if not normalized_query:
            return {"query": query, "matches": []}

        query_tokens = [token for token in normalized_query.split() if len(token) >= 2]
        matches = []
        for index, block in enumerate(flat_blocks):
            normalized_text = _normalize_evidence_text(str(block.get("text") or ""))
            if not normalized_text:
                continue
            exact = normalized_query in normalized_text
            token_match = bool(query_tokens) and all(token in normalized_text for token in query_tokens)
            if not exact and not token_match:
                continue
            nearby = []
            for item in flat_blocks[max(0, index - 2): min(len(flat_blocks), index + 3)]:
                if item.get("page") != block.get("page"):
                    continue
                nearby.append({
                    "block_id": item["block_id"],
                    "text": item["text"],
                })
            matches.append({
                "block_id": block["block_id"],
                "page": block["page"],
                "text": block["text"],
                "confidence": block["confidence"],
                "nearby": nearby,
            })
            if len(matches) >= bounded_results:
                break
        return {"query": query, "matches": matches}

    @tool
    def get_table_rows() -> dict[str, Any]:
        """Read a bounded sample of recognized table rows for transaction mapping review."""
        pages_out = []
        for page in pages[:5]:
            rows = parse_table_html(page.get("table_html"))
            pages_out.append({
                "page": page.get("page"),
                "rows": rows[:12],
            })
        return {"pages": pages_out}

    return [
        get_validation_issues,
        get_current_canonical,
        search_ocr,
        get_ocr_blocks,
        get_table_rows,
    ]


def _tool_observations(messages: list[BaseMessage]) -> list[dict[str, Any]]:
    observations: list[dict[str, Any]] = []
    total_chars = 0
    for message in messages:
        if not isinstance(message, ToolMessage):
            continue
        content = str(message.content)
        remaining = max(0, 18000 - total_chars)
        if remaining <= 0:
            break
        clipped = content[: min(6000, remaining)]
        observations.append({
            "tool": message.name or "tool",
            "content": clipped,
        })
        total_chars += len(clipped)
    return observations


def _agent_trace(messages: list[BaseMessage], model_turns: int) -> dict[str, Any]:
    calls: list[dict[str, Any]] = []
    observations: list[dict[str, Any]] = []
    for message in messages:
        if isinstance(message, AIMessage):
            for call in message.tool_calls:
                calls.append({
                    "name": str(call.get("name") or ""),
                    "args": call.get("args") if isinstance(call.get("args"), dict) else {},
                })
        elif isinstance(message, ToolMessage):
            observations.append({
                "tool": message.name or "tool",
                "content": str(message.content)[:1000],
            })
    return {
        "model_turns": model_turns,
        "tool_call_count": len(calls),
        "tool_calls": calls[:12],
        "observations": observations[:12],
    }


def _run_langgraph_review(
    base_url: str,
    model: str,
    *,
    document_type: str,
    canonical: dict[str, Any],
    validation: dict[str, Any],
    pages: list[dict[str, Any]],
) -> dict[str, Any]:
    max_turns = max(2, min(8, int(os.getenv("AGENT_MAX_TURNS", "5"))))
    max_tool_calls = max(2, min(16, int(os.getenv("AGENT_MAX_TOOL_CALLS", "8"))))
    num_ctx = max(4096, min(32768, int(os.getenv("AGENT_NUM_CTX", "8192"))))
    keep_alive = os.getenv("AGENT_KEEP_ALIVE", "2m").strip() or "2m"

    tools = _make_review_tools(
        canonical=canonical,
        validation=validation,
        pages=pages,
    )
    llm = ChatOllama(
        model=model,
        base_url=base_url,
        temperature=0,
        num_ctx=num_ctx,
        keep_alive=keep_alive,
        validate_model_on_init=True,
    )
    tool_llm = llm.bind_tools(tools)
    structured_llm = llm.with_structured_output(
        _DocumentPatchSchema,
        method="json_schema",
        include_raw=True,
    )

    def agent_node(state: _ReviewGraphState) -> dict[str, Any]:
        response = tool_llm.invoke(state["messages"])
        return {
            "messages": [response],
            "model_turns": int(state.get("model_turns") or 0) + 1,
        }

    def nudge_node(state: _ReviewGraphState) -> dict[str, Any]:
        return {
            "messages": [
                HumanMessage(
                    content=(
                        "You have not inspected document evidence yet. Use the read-only tools now. "
                        "Start with validation issues, then search OCR or inspect OCR blocks for each missing/suspicious field."
                    )
                )
            ]
        }

    def route_after_agent(state: _ReviewGraphState) -> Literal["tools", "nudge", "finalize"]:
        messages = state.get("messages") or []
        last = messages[-1] if messages else None
        turns = int(state.get("model_turns") or 0)
        total_calls = sum(
            len(message.tool_calls)
            for message in messages
            if isinstance(message, AIMessage)
        )
        has_observation = any(isinstance(message, ToolMessage) for message in messages)

        if isinstance(last, AIMessage) and last.tool_calls:
            if turns >= max_turns or total_calls > max_tool_calls:
                return "finalize"
            return "tools"

        if not has_observation and turns < min(max_turns, 2):
            return "nudge"
        return "finalize"

    def finalize_node(state: _ReviewGraphState) -> dict[str, Any]:
        observations = _tool_observations(state.get("messages") or [])
        final_prompt = {
            "current_canonical": _compact_canonical(canonical),
            "validation": _compact_validation(validation),
            "tool_observations": observations,
        }
        result = structured_llm.invoke([
            SystemMessage(
                content=(
                    "Create the final conservative supplier-statement correction proposal. "
                    "Use only values and OCR block IDs that appear in tool observations. "
                    "Every field_updates entry must have evidence block IDs. "
                    "Only remove a current field when the observed OCR context clearly proves it is a false positive. "
                    "Do not rewrite transaction rows. If evidence is insufficient, return no update and lower confidence."
                )
            ),
            HumanMessage(
                content=json.dumps(final_prompt, separators=(",", ":"), ensure_ascii=False)
            ),
        ])
        parsed = result.get("parsed") if isinstance(result, dict) else result
        parsing_error = result.get("parsing_error") if isinstance(result, dict) else None
        if parsing_error is not None:
            raise RuntimeError(f"structured proposal parsing failed: {parsing_error}")
        if isinstance(parsed, _DocumentPatchSchema):
            payload = parsed.model_dump()
        elif isinstance(parsed, dict):
            payload = parsed
        else:
            raise RuntimeError("structured proposal returned no parsed payload")
        return {"final_payload": payload}

    tool_node = ToolNode(
        tools,
        handle_tool_errors=lambda exc: f"Read-only tool failed: {type(exc).__name__}: {exc}",
    )

    graph = StateGraph(_ReviewGraphState)
    graph.add_node("agent", agent_node)
    graph.add_node("nudge", nudge_node)
    graph.add_node("tools", tool_node)
    graph.add_node("finalize", finalize_node)
    graph.add_edge(START, "agent")
    graph.add_conditional_edges(
        "agent",
        route_after_agent,
        {
            "tools": "tools",
            "nudge": "nudge",
            "finalize": "finalize",
        },
    )
    graph.add_edge("nudge", "agent")
    graph.add_edge("tools", "agent")
    graph.add_edge("finalize", END)

    compiled = graph.compile()
    initial_state: _ReviewGraphState = {
        "messages": [
            SystemMessage(content=_system_prompt(document_type)),
            HumanMessage(
                content=(
                    "Review this document for missing or suspicious top-level fields. "
                    "Use validation/canonical tools first, then targeted OCR searches. "
                    "Stop calling tools once you have enough evidence for a conservative proposal."
                )
            ),
        ],
        "model_turns": 0,
        "final_payload": None,
        "error": None,
    }
    result = compiled.invoke(
        initial_state,
        config={"recursion_limit": max(16, max_turns * 3 + 4)},
    )
    payload = result.get("final_payload")
    if not isinstance(payload, dict):
        raise RuntimeError("LangGraph document reviewer returned no final payload")
    payload["_agent_trace"] = _agent_trace(
        result.get("messages") or [],
        int(result.get("model_turns") or 0),
    )
    return payload


def _proposal_score(proposal: DocumentReviewProposal) -> tuple[int, int, float]:
    required = {"supplier_name", "closing_balance"}
    required_updates = sum(1 for name in required if name in proposal.field_updates)
    changes = len(proposal.field_updates) + len(proposal.field_removals)
    return (required_updates, changes, proposal.confidence)


def _proposal_acceptable(
    proposal: DocumentReviewProposal,
    *,
    canonical: dict[str, Any],
) -> bool:
    threshold = max(0.0, min(1.0, float(os.getenv("AGENT_MIN_CONFIDENCE", "0.90"))))
    if proposal.confidence < threshold:
        return False
    if not (proposal.field_updates or proposal.field_removals):
        return False

    current_fields = {
        str(name): str((field or {}).get("value") or "").strip()
        for name, field in (canonical.get("fields") or {}).items()
        if isinstance(field, dict)
    }
    for field in proposal.field_removals:
        current_fields.pop(field, None)
    current_fields.update({
        name: str(value).strip()
        for name, value in proposal.field_updates.items()
    })
    return all(current_fields.get(name) for name in ("supplier_name", "closing_balance"))


def review_document(
    *,
    document_type: str,
    canonical: dict[str, Any],
    validation: dict[str, Any],
    pages: list[dict[str, Any]],
) -> DocumentReviewProposal:
    provider = os.getenv("AGENT_PROVIDER", "disabled").strip().lower()
    document_type = document_type.strip().lower()

    if provider == "disabled":
        raise RuntimeError("agent fallback provider is disabled")
    if document_type != "supplier_statement":
        raise RuntimeError(f"document review is not enabled for {document_type!r}")
    if provider != "ollama":
        raise RuntimeError("document-reviewer-v2 currently requires AGENT_PROVIDER=ollama")

    base_url = os.getenv("AGENT_BASE_URL", "http://localhost:11434").strip()
    primary_model = os.getenv("AGENT_MODEL", "ministral-3:8b").strip() or "ministral-3:8b"
    fallback_model = os.getenv("AGENT_FALLBACK_MODEL", "qwen3.5:9b").strip()

    primary_error: str | None = None
    primary: DocumentReviewProposal | None = None
    try:
        primary_payload = _run_langgraph_review(
            base_url,
            primary_model,
            document_type=document_type,
            canonical=canonical,
            validation=validation,
            pages=pages,
        )
        primary = _sanitize_proposal(
            primary_payload,
            provider=provider,
            model=primary_model,
            document_type=document_type,
            pages=pages,
        )
        primary.prompt_version = "document-reviewer-langgraph-v2"
        if _proposal_acceptable(primary, canonical=canonical):
            return primary
    except Exception as exc:
        primary_error = str(exc)[:500]

    fallback: DocumentReviewProposal | None = None
    fallback_error: str | None = None
    if fallback_model and fallback_model != primary_model:
        try:
            fallback_payload = _run_langgraph_review(
                base_url,
                fallback_model,
                document_type=document_type,
                canonical=canonical,
                validation=validation,
                pages=pages,
            )
            fallback = _sanitize_proposal(
                fallback_payload,
                provider=provider,
                model=fallback_model,
                document_type=document_type,
                pages=pages,
            )
            fallback.prompt_version = "document-reviewer-langgraph-v2-fallback"
        except Exception as exc:
            fallback_error = str(exc)[:500]

    candidates = [proposal for proposal in (primary, fallback) if proposal is not None]
    if candidates:
        selected = max(candidates, key=_proposal_score)
        selected.raw_response.setdefault("_review_errors", {
            "primary": primary_error,
            "fallback": fallback_error,
        })
        return selected

    details = "; ".join(
        value for value in (
            f"primary={primary_error}" if primary_error else "",
            f"fallback={fallback_error}" if fallback_error else "",
        )
        if value
    )
    raise RuntimeError(f"LangGraph document review failed: {details or 'no proposal'}")


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

    transaction_rows = ((result.get("tables") or {}).get("transactions") or [])
    required_transaction_fields = {"date", "invoice_number", "debit", "credit", "balance"}
    incomplete_rows = [
        index + 1
        for index, row in enumerate(transaction_rows)
        if not required_transaction_fields.issubset(set(row))
    ]
    transaction_mapping_incomplete = bool(transaction_rows) and bool(incomplete_rows)

    result["requires_review"] = bool(missing or transaction_mapping_incomplete)
    validation_summary = result.setdefault("validation", {})
    validation_summary["missing_required_fields"] = missing
    validation_summary["transaction_mapping_incomplete"] = transaction_mapping_incomplete
    validation_summary["incomplete_transaction_rows"] = incomplete_rows
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
        "trace": proposal.raw_response.get("_agent_trace", {}),
        "review_errors": proposal.raw_response.get("_review_errors", {}),
    }
    return result
