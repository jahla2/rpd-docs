from __future__ import annotations

import importlib.util
from dataclasses import asdict
from functools import lru_cache
from pathlib import Path
from typing import Any

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

from app.extraction.invoice import extract_invoice
from app.ocr.rapidocr_adapter import RapidOCREngine
from app.tables.slanet_adapter import SLANetTableEngine

app = FastAPI(title="RPD AI Engine", version="0.2.0")


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


class InvoiceExtractRequest(BaseModel):
    document_id: str
    page: int = Field(default=1, ge=1)
    ocr_blocks: list[OCRBlockInput]
    table_html: str | None = None


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
        "ready": rapidocr_available,
        "checks": {
            "rapidocr": rapidocr_available,
            "rapid_table": rapidtable_available,
        },
    }


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

    try:
        result = table_engine().process(
            str(path),
            ocr_blocks=[block.model_dump() for block in request.ocr_blocks],
        )
    except RuntimeError as exc:
        raise HTTPException(status_code=503, detail=str(exc)) from exc
    except Exception as exc:
        raise HTTPException(status_code=500, detail=f"TABLE_RECOGNITION_FAILED: {exc}") from exc

    response = asdict(result)
    response["document_id"] = request.document_id
    response["page"] = request.page
    return response


@app.post("/v1/extract/invoice")
def run_invoice_extraction(request: InvoiceExtractRequest) -> dict[str, Any]:
    return extract_invoice(
        document_id=request.document_id,
        page=request.page,
        ocr_blocks=[block.model_dump() for block in request.ocr_blocks],
        table_html=request.table_html,
    )
