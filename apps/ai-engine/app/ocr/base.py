from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass


@dataclass(slots=True)
class OCRBlock:
    block_id: str
    text: str
    bbox: list[float]
    confidence: float


@dataclass(slots=True)
class OCRPage:
    schema_version: str
    document_id: str
    page: int
    engine: str
    model: str | None
    blocks: list[OCRBlock]


class OCREngine(ABC):
    @abstractmethod
    def process(self, image_path: str, document_id: str, page: int) -> OCRPage:
        raise NotImplementedError
