from __future__ import annotations

import importlib.util
import os
import time
from dataclasses import asdict
from functools import lru_cache
from pathlib import Path
from tempfile import TemporaryDirectory
from typing import Any

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

from app.extraction.invoice import extract_invoice, extract_invoice_document
from app.extraction.financial import (
    SUPPORTED_FINANCIAL_TYPES,
    extract_financial_document,
    field_aliases,
    table_aliases,
)
from app.layout.profile import build_layout_fingerprint, match_layout_profile
from app.agents.invoice_resolver import apply_proposal, resolve_invoice
from app.agents.document_reviewer import apply_document_proposal, review_document
from app.agents.glm_ocr_reviewer import glm_ocr_useful, review_with_glm_ocr
from app.classification.service import classification_service
from app.pdftext.poppler import parse_bbox_layout
from app.ocr.rapidocr_adapter import RapidOCREngine
from app.tables.region import detect_table_region, translate_ocr_blocks
from app.tables.slanet_adapter import SLANetTableEngine

app = FastAPI(title="RPD AI Engine", version="0.3.0")


class OCRRequest(BaseModel):
    document_id: str
    page: int = Field(default=1, ge=1)
    image_path: str


class OCRBlockInput(BaseModel):
    block_id: str
    text: str
    bbox: list[float]
    confidence: float = Field(ge=0, le=1)


class TableRequest(BaseModel):
    document_id: str
    page: int = Field(default=1, ge=1)
    image_path: str
    ocr_blocks: list[OCRBlockInput] = Field(default_factory=list)
    auto_crop: bool = True
    document_type: str | None = None
    layout_config: dict[str, Any] | None = None


class InvoiceExtractRequest(BaseModel):
    document_id: str
    page: int = Field(default=1, ge=1)
    ocr_blocks: list[OCRBlockInput]
    table_html: str | None = None


class PDFTextRequest(BaseModel):
    document_id: str
    page: int = Field(default=1, ge=1)
    layout_path: str
    image_path: str


class InvoicePageInput(BaseModel):
    page: int = Field(ge=1)
    ocr_blocks: list[OCRBlockInput] = Field(default_factory=list)
    table_html: str | None = None


class InvoiceDocumentExtractRequest(BaseModel):
    document_id: str
    pages: list[InvoicePageInput]
    mapping_config: dict[str, Any] | None = None


class FinancialPageInput(BaseModel):
    page: int = Field(ge=1)
    image_path: str | None = None
    image_width: int | None = Field(default=None, ge=1)
    image_height: int | None = Field(default=None, ge=1)
    ocr_blocks: list[OCRBlockInput] = Field(default_factory=list)
    table_html: str | None = None
    table_cell_bboxes: list[Any] = Field(default_factory=list)
    table_logic_points: list[Any] = Field(default_factory=list)


class FinancialDocumentExtractRequest(BaseModel):
    document_id: str
    document_type: str
    pages: list[FinancialPageInput]
    mapping_config: dict[str, Any] | None = None
    layout_match: dict[str, Any] | None = None


class LayoutMatchPageInput(BaseModel):
    page: int = Field(ge=1)
    image_path: str | None = None
    image_width: int | None = Field(default=None, ge=1)
    image_height: int | None = Field(default=None, ge=1)
    ocr_blocks: list[OCRBlockInput] = Field(default_factory=list)


class LayoutMatchRequest(BaseModel):
    document_id: str
    document_type: str
    pages: list[LayoutMatchPageInput]
    profiles: list[dict[str, Any]] = Field(default_factory=list)


class ClassificationPageInput(BaseModel):
    page: int = Field(ge=1)
    ocr_blocks: list[OCRBlockInput] = Field(default_factory=list)


class ClassificationRequest(BaseModel):
    document_id: str
    pages: list[ClassificationPageInput]
    document_type_hint: str | None = None
    hint_source: str | None = None


@lru_cache(maxsize=1)
def ocr_engine() -> RapidOCREngine:
    return RapidOCREngine()


@lru_cache(maxsize=1)
def table_engine() -> SLANetTableEngine:
    return SLANetTableEngine()


@app.get("/health/live")
def live() -> dict:
    return {"status": "ok", "service": "rpd-ai-engine"}


@app.get("/health/ready")
def ready() -> dict:
    rapidocr_available = importlib.util.find_spec("rapidocr") is not None
    rapidtable_available = importlib.util.find_spec("rapid_table") is not None
    classifier = classification_service().health()
    return {
        "ready": rapidocr_available and rapidtable_available and bool(classifier.get("ready")),
        "checks": {
            "rapidocr": rapidocr_available,
            "rapid_table": rapidtable_available,
            "document_classifier": classifier,
        },
    }


@app.post("/v1/classify")
def run_classification(request: ClassificationRequest) -> dict[str, Any]:
    if not request.pages:
        raise HTTPException(status_code=400, detail="pages must not be empty")
    return classification_service().classify(
        document_id=request.document_id,
        pages=[
            {
                "page": page.page,
                "ocr_blocks": [block.model_dump() for block in page.ocr_blocks],
            }
            for page in request.pages
        ],
        document_type_hint=request.document_type_hint,
        hint_source=request.hint_source,
    )


@app.post("/v1/layout/match")
def run_layout_match(request: LayoutMatchRequest) -> dict[str, Any]:
    document_type = request.document_type.strip().lower()
    if document_type not in SUPPORTED_FINANCIAL_TYPES:
        raise HTTPException(status_code=400, detail=f"unsupported financial document type: {document_type}")
    if not request.pages:
        raise HTTPException(status_code=400, detail="pages must not be empty")

    pages = [
        {
            "page": page.page,
            "image_path": page.image_path,
            "image_width": page.image_width,
            "image_height": page.image_height,
            "ocr_blocks": [block.model_dump() for block in page.ocr_blocks],
        }
        for page in request.pages
    ]
    fingerprint = build_layout_fingerprint(
        document_type,
        pages,
        field_aliases(document_type),
        table_aliases(document_type),
    )
    return match_layout_profile(fingerprint, request.profiles)


@app.post("/v1/ocr")
def run_ocr(request: OCRRequest) -> dict:
    path = Path(request.image_path)
    if not path.exists() or not path.is_file():
        raise HTTPException(status_code=400, detail="image_path does not exist")

    try:
        result = ocr_engine().process(str(path), document_id=request.document_id, page=request.page)
    except RuntimeError as exc:
        raise HTTPException(status_code=503, detail=str(exc)) from exc
    except Exception as exc:
        raise HTTPException(status_code=500, detail=f"OCR_FAILED: {exc}") from exc

    return asdict(result)


@app.post("/v1/tables")
def run_tables(request: TableRequest) -> dict:
    path = Path(request.image_path)
    if not path.exists() or not path.is_file():
        raise HTTPException(status_code=400, detail="image_path does not exist")

    blocks = [block.model_dump() for block in request.ocr_blocks]
    table_input_path = str(path)
    table_blocks = blocks
    region_bbox: list[int] | None = None
    temp_dir: TemporaryDirectory | None = None

    try:
        if request.auto_crop and blocks:
            try:
                from PIL import Image
            except ImportError as exc:
                raise RuntimeError("Pillow is required for automatic table cropping") from exc

            with Image.open(path) as image:
                width, height = image.size
                header_aliases = None
                document_type = (request.document_type or "").strip().lower()
                if document_type in SUPPORTED_FINANCIAL_TYPES:
                    header_aliases = table_aliases(document_type, request.layout_config)
                region_bbox = detect_table_region(
                    blocks,
                    width,
                    height,
                    header_aliases=header_aliases,
                )

                if region_bbox is not None:
                    temp_dir = TemporaryDirectory(prefix="rpd-table-")
                    crop_path = Path(temp_dir.name) / "table-region.png"
                    image.crop(tuple(region_bbox)).save(crop_path)
                    table_input_path = str(crop_path)
                    table_blocks = translate_ocr_blocks(blocks, region_bbox)

        result = table_engine().process(
            table_input_path,
            ocr_blocks=table_blocks,
        )
    except RuntimeError as exc:
        raise HTTPException(status_code=503, detail=str(exc)) from exc
    except Exception as exc:
        raise HTTPException(status_code=500, detail=f"TABLE_RECOGNITION_FAILED: {exc}") from exc
    finally:
        if temp_dir is not None:
            temp_dir.cleanup()

    response = asdict(result)
    response["document_id"] = request.document_id
    response["page"] = request.page
    response["region_bbox"] = region_bbox
    response["ocr_reused"] = len(table_blocks) > 0
    return response


@app.post("/v1/extract/invoice")
def run_invoice_extraction(request: InvoiceExtractRequest) -> dict[str, Any]:
    return extract_invoice(
        document_id=request.document_id,
        page=request.page,
        ocr_blocks=[block.model_dump() for block in request.ocr_blocks],
        table_html=request.table_html,
    )


@app.post("/v1/pdftext")
def run_pdf_text(request: PDFTextRequest) -> dict:
    try:
        return parse_bbox_layout(
            layout_path=request.layout_path,
            image_path=request.image_path,
            document_id=request.document_id,
            page=request.page,
        )
    except (ValueError, RuntimeError) as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc
    except Exception as exc:
        raise HTTPException(status_code=500, detail=f"PDF_TEXT_FAILED: {exc}") from exc


@app.post("/v1/extract/invoice-document")
def run_invoice_document_extraction(request: InvoiceDocumentExtractRequest) -> dict[str, Any]:
    if not request.pages:
        raise HTTPException(status_code=400, detail="pages must not be empty")
    return extract_invoice_document(
        document_id=request.document_id,
        pages=[
            {
                "page": page.page,
                "ocr_blocks": [block.model_dump() for block in page.ocr_blocks],
                "table_html": page.table_html,
            }
            for page in request.pages
        ],
        mapping_config=request.mapping_config,
    )


@app.post("/v1/extract/financial-document")
def run_financial_document_extraction(request: FinancialDocumentExtractRequest) -> dict[str, Any]:
    document_type = request.document_type.strip().lower()
    if document_type not in SUPPORTED_FINANCIAL_TYPES:
        raise HTTPException(status_code=400, detail=f"unsupported financial document type: {document_type}")
    if not request.pages:
        raise HTTPException(status_code=400, detail="pages must not be empty")
    try:
        return extract_financial_document(
            document_id=request.document_id,
            document_type=document_type,
            pages=[
                {
                    "page": page.page,
                    "image_path": page.image_path,
                    "image_width": page.image_width,
                    "image_height": page.image_height,
                    "ocr_blocks": [block.model_dump() for block in page.ocr_blocks],
                    "table_html": page.table_html,
                    "table_cell_bboxes": page.table_cell_bboxes,
                    "table_logic_points": page.table_logic_points,
                }
                for page in request.pages
            ],
            mapping_config=request.mapping_config,
            layout_match=request.layout_match,
        )
    except ValueError as exc:
        raise HTTPException(status_code=400, detail=str(exc)) from exc


class AgentInvoiceResolveRequest(BaseModel):
    document_id: str
    trigger_code: str
    canonical: dict[str, Any]
    validation: dict[str, Any]
    pages: list[dict[str, Any]] = Field(default_factory=list)


class AgentDocumentReviewRequest(BaseModel):
    document_id: str
    document_type: str
    trigger_code: str
    canonical: dict[str, Any]
    validation: dict[str, Any]
    pages: list[dict[str, Any]] = Field(default_factory=list)


@app.post("/v1/agent/resolve-invoice")
def run_agent_invoice_resolution(request: AgentInvoiceResolveRequest) -> dict[str, Any]:
    try:
        proposal = resolve_invoice(
            canonical=request.canonical,
            validation=request.validation,
            pages=request.pages,
            trigger_code=request.trigger_code,
        )
        resolved = apply_proposal(request.canonical, proposal)
        return {
            "provider": proposal.provider,
            "model": proposal.model,
            "prompt_version": proposal.prompt_version,
            "confidence": proposal.confidence,
            "field_updates": proposal.field_updates,
            "evidence_block_ids": proposal.evidence_block_ids,
            "summary": proposal.summary,
            "resolved_canonical": resolved,
        }
    except RuntimeError as exc:
        raise HTTPException(status_code=503, detail=str(exc)) from exc
    except Exception as exc:
        raise HTTPException(status_code=502, detail=f"AGENT_RESOLUTION_FAILED: {exc}") from exc


@app.post("/v1/agent/review-document")
def run_agent_document_review(request: AgentDocumentReviewRequest) -> dict[str, Any]:
    try:
        tier = "qwen_fallback"
        escalated = False
        glm_elapsed_ms: int | None = None
        glm_error: str | None = None
        proposal = None

        glm_enabled = os.getenv("GLM_OCR_ENABLED", "true").strip().lower() in {"1", "true", "yes", "on"}
        if glm_enabled and request.document_type.strip().lower() == "supplier_statement":
            try:
                proposal, glm_elapsed_ms = review_with_glm_ocr(
                    document_type=request.document_type,
                    canonical=request.canonical,
                    validation=request.validation,
                    pages=request.pages,
                )
                if glm_ocr_useful(proposal):
                    tier = "glm_ocr"
                else:
                    proposal = None
                    escalated = True
            except Exception as exc:
                glm_error = str(exc)[:500]
                proposal = None
                escalated = True

        fallback_elapsed_ms: int | None = None
        if proposal is None:
            started = time.perf_counter()
            proposal = review_document(
                document_type=request.document_type,
                canonical=request.canonical,
                validation=request.validation,
                pages=request.pages,
            )
            fallback_elapsed_ms = int((time.perf_counter() - started) * 1000)

        resolved = apply_document_proposal(request.canonical, proposal, request.pages)
        return {
            "provider": proposal.provider,
            "model": proposal.model,
            "prompt_version": proposal.prompt_version,
            "confidence": proposal.confidence,
            "review_tier": tier,
            "escalated": escalated,
            "glm_elapsed_ms": glm_elapsed_ms,
            "fallback_elapsed_ms": fallback_elapsed_ms,
            "glm_error": glm_error,
            "field_updates": proposal.field_updates,
            "field_removals": proposal.field_removals,
            "evidence": proposal.evidence,
            "suggestions": proposal.suggestions,
            "summary": proposal.summary,
            "resolved_canonical": resolved,
        }
    except RuntimeError as exc:
        raise HTTPException(status_code=503, detail=str(exc)) from exc
    except Exception as exc:
        raise HTTPException(status_code=502, detail=f"DOCUMENT_REVIEW_FAILED: {exc}") from exc
