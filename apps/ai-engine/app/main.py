from __future__ import annotations

import importlib.util
from dataclasses import asdict
from functools import lru_cache
from pathlib import Path
from tempfile import TemporaryDirectory
from typing import Any

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

from app.extraction.invoice import extract_invoice, extract_invoice_document
from app.agents.invoice_resolver import apply_proposal, resolve_invoice
from app.classification.rules import classify_document
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
    return {
        "ready": rapidocr_available and rapidtable_available,
        "checks": {
            "rapidocr": rapidocr_available,
            "rapid_table": rapidtable_available,
            "classification_rules": True,
        },
    }


@app.post("/v1/classify")
def run_classification(request: ClassificationRequest) -> dict[str, Any]:
    if not request.pages:
        raise HTTPException(status_code=400, detail="pages must not be empty")
    return classify_document(
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
                region_bbox = detect_table_region(blocks, width, height)

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


class AgentInvoiceResolveRequest(BaseModel):
    document_id: str
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
