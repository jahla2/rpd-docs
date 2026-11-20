from __future__ import annotations

import base64
import json
import os
import time
from pathlib import Path
from typing import Any

import httpx

from app.agents.document_reviewer import (
    DocumentReviewProposal,
    SUPPLIER_STATEMENT_FIELDS,
    _block_lookup,
    _sanitize_proposal,
    _value_supported,
)


def _json_payload(text: str) -> dict[str, Any]:
    text = text.strip()
    if text.startswith("```"):
        lines = text.splitlines()
        if lines and lines[0].startswith("```"):
            lines = lines[1:]
        if lines and lines[-1].strip() == "```":
            lines = lines[:-1]
        text = "\n".join(lines).strip()

    try:
        value = json.loads(text)
    except json.JSONDecodeError:
        start = text.find("{")
        end = text.rfind("}")
        if start < 0 or end <= start:
            raise RuntimeError("GLM-OCR returned non-JSON output")
        value = json.loads(text[start : end + 1])

    if not isinstance(value, dict):
        raise RuntimeError("GLM-OCR returned a non-object JSON payload")
    return value


def _compact_context(
    canonical: dict[str, Any],
    validation: dict[str, Any],
) -> dict[str, Any]:
    fields = {}
    for name, field in (canonical.get("fields") or {}).items():
        if not isinstance(field, dict):
            continue
        fields[str(name)] = {
            "value": field.get("value"),
            "raw": field.get("raw"),
        }

    transactions = ((canonical.get("tables") or {}).get("transactions") or [])
    compact_rows = []
    for row in transactions[:8]:
        if isinstance(row, dict):
            compact_rows.append({str(key): value for key, value in row.items() if key != "page"})

    issues = []
    for rule in validation.get("rules") or []:
        if not isinstance(rule, dict):
            continue
        if str(rule.get("status") or "").upper() not in {"FAIL", "WARN"}:
            continue
        issues.append({
            "code": rule.get("code"),
            "status": rule.get("status"),
            "message": rule.get("message"),
        })

    return {
        "document_type": canonical.get("document_type"),
        "fields": fields,
        "transactions": compact_rows,
        "validation_issues": issues[:12],
    }


def _find_evidence(
    field: str,
    value: str,
    pages: list[dict[str, Any]],
) -> list[str]:
    data_type = SUPPLIER_STATEMENT_FIELDS.get(field)
    if data_type is None:
        return []

    blocks = list(_block_lookup(pages).values())
    for block in blocks:
        if _value_supported(value, data_type, [block]):
            return [str(block["block_id"])]

    for index in range(len(blocks) - 1):
        pair = [blocks[index], blocks[index + 1]]
        if pair[0].get("page") != pair[1].get("page"):
            continue
        if _value_supported(value, data_type, pair):
            return [str(pair[0]["block_id"]), str(pair[1]["block_id"])]

    return []


def _attach_evidence(
    payload: dict[str, Any],
    pages: list[dict[str, Any]],
) -> dict[str, Any]:
    output = dict(payload)
    updates = output.get("field_updates") or {}
    if not isinstance(updates, dict):
        updates = {}

    evidence: dict[str, list[str]] = {}
    for field, raw_value in updates.items():
        if field not in SUPPLIER_STATEMENT_FIELDS:
            continue
        if not isinstance(raw_value, (str, int, float)):
            continue
        value = str(raw_value).strip()
        if not value:
            continue
        block_ids = _find_evidence(field, value, pages)
        if block_ids:
            evidence[field] = block_ids

    output["evidence"] = evidence
    return output


def _select_images(
    pages: list[dict[str, Any]],
    max_pages: int,
) -> tuple[list[str], list[int]]:
    images: list[str] = []
    page_numbers: list[int] = []

    for page in sorted(pages, key=lambda item: int(item.get("page") or 0)):
        raw_path = str(page.get("image_path") or "").strip()
        if not raw_path:
            continue
        path = Path(raw_path)
        if not path.exists() or not path.is_file():
            continue
        images.append(base64.b64encode(path.read_bytes()).decode("ascii"))
        page_numbers.append(int(page.get("page") or 0))
        if len(images) >= max_pages:
            break

    return images, page_numbers


def _prompt(context: dict[str, Any], page_numbers: list[int]) -> str:
    return (
        "Review the attached supplier statement image directly. "
        "This is a verification pass after deterministic OCR/extraction. "
        "Return JSON only, no markdown. Do not invent values. "
        "Only propose values that are visibly present on the page. "
        "Allowed fields: supplier_name, account_number, statement_date, currency, opening_balance, closing_balance. "
        "If the current account_number is text from a bank-account-change warning rather than a real customer account number, "
        "include account_number in field_removals. "
        "Do not rewrite transaction rows; report table/mapping problems in suggestions. "
        "Use ISO YYYY-MM-DD for dates and plain decimal strings for amounts. "
        "Return exactly this shape: "
        "{\"confidence\":0.0,\"field_updates\":{},\"field_removals\":[],\"suggestions\":[],\"summary\":\"\"}. "
        f"Attached page numbers: {page_numbers}. "
        "Current deterministic context: "
        + json.dumps(context, separators=(",", ":"), ensure_ascii=False)
    )


def review_with_glm_ocr(
    *,
    document_type: str,
    canonical: dict[str, Any],
    validation: dict[str, Any],
    pages: list[dict[str, Any]],
) -> tuple[DocumentReviewProposal, int]:
    document_type = document_type.strip().lower()
    if document_type != "supplier_statement":
        raise RuntimeError(f"GLM-OCR review is not enabled for {document_type!r}")

    provider = os.getenv("AGENT_PROVIDER", "disabled").strip().lower()
    if provider != "ollama":
        raise RuntimeError("GLM-OCR reviewer requires AGENT_PROVIDER=ollama")

    base_url = os.getenv("AGENT_BASE_URL", "http://localhost:11434").strip()
    model = os.getenv("GLM_OCR_MODEL", "glm-ocr:q8_0").strip() or "glm-ocr:q8_0"
    max_pages = max(1, min(3, int(os.getenv("GLM_OCR_MAX_PAGES", "1"))))
    num_ctx = max(2048, min(8192, int(os.getenv("GLM_OCR_NUM_CTX", "4096"))))
    keep_alive = os.getenv("GLM_OCR_KEEP_ALIVE", "5m").strip() or "5m"

    images, page_numbers = _select_images(pages, max_pages)
    if not images:
        raise RuntimeError("GLM-OCR reviewer could not access any page image")

    context = _compact_context(canonical, validation)
    started = time.perf_counter()
    response = httpx.post(
        base_url.rstrip("/") + "/api/chat",
        timeout=90.0,
        json={
            "model": model,
            "stream": False,
            "format": "json",
            "keep_alive": keep_alive,
            "options": {
                "temperature": 0,
                "num_ctx": num_ctx,
            },
            "messages": [
                {
                    "role": "user",
                    "content": _prompt(context, page_numbers),
                    "images": images,
                }
            ],
        },
    )
    response.raise_for_status()
    elapsed_ms = int((time.perf_counter() - started) * 1000)

    outer = response.json()
    content = str(((outer.get("message") or {}).get("content") or "")).strip()
    payload = _attach_evidence(_json_payload(content), pages)
    proposal = _sanitize_proposal(
        payload,
        provider="ollama",
        model=model,
        document_type=document_type,
        pages=pages,
    )
    proposal.prompt_version = "glm-ocr-document-review-v1"
    return proposal, elapsed_ms


def glm_ocr_useful(proposal: DocumentReviewProposal) -> bool:
    glm_threshold = float(os.getenv("GLM_OCR_MIN_CONFIDENCE", "0.90"))
    agent_threshold = float(os.getenv("AGENT_MIN_CONFIDENCE", "0.90"))
    threshold = max(glm_threshold, agent_threshold)
    return (
        proposal.confidence >= max(0.0, min(1.0, threshold))
        and bool(proposal.field_updates or proposal.field_removals)
    )
