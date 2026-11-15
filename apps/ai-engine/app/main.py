from __future__ import annotations

import importlib.util
from dataclasses import asdict
from functools import lru_cache
from pathlib import Path

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel, Field

from app.ocr.rapidocr_adapter import RapidOCREngine

app = FastAPI(title="RPD AI Engine", version="0.1.0")


class OCRRequest(BaseModel):
    document_id: str
    page: int = Field(default=1, ge=1)
    image_path: str


@lru_cache(maxsize=1)
def ocr_engine() -> RapidOCREngine:
    return RapidOCREngine()


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
        result = ocr_engine().process(
            str(path),
            document_id=request.document_id,
            page=request.page,
        )
    except RuntimeError as exc:
        raise HTTPException(status_code=503, detail=str(exc)) from exc
    except Exception as exc:
        raise HTTPException(status_code=500, detail=f"OCR_FAILED: {exc}") from exc

    return asdict(result)
