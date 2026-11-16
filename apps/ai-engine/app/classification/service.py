from __future__ import annotations

import os
import threading
import time
from functools import lru_cache
from typing import Any

from .modernbert import ModernBERTZeroShotClassifier
from .rules import build_document_text, classify_document


class ClassificationService:
    def __init__(
        self,
        *,
        backend: str = "rules",
        model_dir: str = "/models/modernbert",
        model_file: str = "onnx/model_int8.onnx",
        model_provider: str = "cpu",
        model_max_length: int = 256,
        rule_accept_confidence: float = 0.90,
        model_accept_confidence: float = 0.72,
        model_min_margin: float = 0.08,
        model_version: str = "modernbert-base-zeroshot-v2.0-int8",
        model: Any | None = None,
    ) -> None:
        self.backend = (backend or "rules").strip().lower()
        if self.backend not in {"rules", "modernbert"}:
            raise RuntimeError(f"unsupported document classifier backend {self.backend!r}")
        self.model_dir = model_dir
        self.model_file = model_file
        self.model_provider = model_provider
        self.model_max_length = model_max_length
        self.rule_accept_confidence = rule_accept_confidence
        self.model_accept_confidence = model_accept_confidence
        self.model_min_margin = model_min_margin
        self.model_version = model_version
        self._model = model
        self._model_lock = threading.Lock()

    @classmethod
    def from_env(cls) -> "ClassificationService":
        return cls(
            backend=os.getenv("DOCUMENT_CLASSIFIER_BACKEND", "rules"),
            model_dir=os.getenv("DOCUMENT_CLASSIFIER_MODEL_DIR", "/models/modernbert"),
            model_file=os.getenv("DOCUMENT_CLASSIFIER_MODEL_FILE", "onnx/model_int8.onnx"),
            model_provider=os.getenv("DOCUMENT_CLASSIFIER_PROVIDER", "cpu"),
            model_max_length=_int_env("DOCUMENT_CLASSIFIER_MAX_LENGTH", 256, 64, 2048),
            rule_accept_confidence=_float_env("DOCUMENT_CLASSIFIER_RULE_THRESHOLD", 0.90, 0.0, 1.0),
            model_accept_confidence=_float_env("DOCUMENT_CLASSIFIER_MODEL_THRESHOLD", 0.72, 0.0, 1.0),
            model_min_margin=_float_env("DOCUMENT_CLASSIFIER_MODEL_MARGIN", 0.08, 0.0, 1.0),
            model_version=os.getenv(
                "DOCUMENT_CLASSIFIER_MODEL_VERSION",
                "modernbert-base-zeroshot-v2.0-int8",
            ),
        )

    def _get_model(self) -> ModernBERTZeroShotClassifier | Any:
        if self.backend != "modernbert":
            raise RuntimeError("ModernBERT backend is not enabled")
        if self._model is not None:
            return self._model
        with self._model_lock:
            if self._model is None:
                self._model = ModernBERTZeroShotClassifier(
                    self.model_dir,
                    model_file=self.model_file,
                    max_length=self.model_max_length,
                    provider=self.model_provider,
                    model_version=self.model_version,
                )
        return self._model

    def health(self) -> dict[str, Any]:
        if self.backend == "rules":
            return {"ready": True, "backend": "rules", "warm": True}
        try:
            model = self._get_model()
            details = model.health() if hasattr(model, "health") else {"ready": True}
            return {"ready": bool(details.get("ready", True)), "backend": self.backend, **details}
        except Exception as exc:
            return {"ready": False, "backend": self.backend, "error": str(exc)}

    def classify(
        self,
        *,
        document_id: str,
        pages: list[dict[str, Any]],
        document_type_hint: str | None = None,
        hint_source: str | None = None,
    ) -> dict[str, Any]:
        started = time.perf_counter_ns()
        rule_result = classify_document(
            document_id=document_id,
            pages=pages,
            document_type_hint=document_type_hint,
            hint_source=hint_source,
        )

        if rule_result["method"] != "deterministic_rules":
            return rule_result
        if (
            rule_result["document_type"] != "unknown"
            and float(rule_result["confidence"]) >= self.rule_accept_confidence
        ):
            return rule_result
        if self.backend != "modernbert":
            return rule_result

        text = build_document_text(pages)
        try:
            model_result = self._get_model().classify(text)
        except Exception as exc:
            degraded = dict(rule_result)
            degraded["fallback_error"] = str(exc)
            degraded["requires_review"] = True
            return degraded

        alternatives = list(model_result.get("alternatives") or [])
        confidence = float(model_result.get("confidence", 0.0))
        margin = confidence
        if len(alternatives) > 1:
            margin = confidence - float(alternatives[1].get("confidence", 0.0))
        accepted = (
            str(model_result.get("document_type", "")) != ""
            and confidence >= self.model_accept_confidence
            and margin >= self.model_min_margin
        )
        total_latency_ms = max(0, (time.perf_counter_ns() - started) // 1_000_000)
        return {
            "schema_version": "1.0",
            "document_id": document_id,
            "document_type": model_result["document_type"] if accepted else "unknown",
            "confidence": round(confidence, 5),
            "method": "modernbert_zeroshot",
            "model": str(model_result.get("model", "")),
            "model_version": str(model_result.get("model_version", self.model_version)),
            "alternatives": alternatives,
            "latency_ms": total_latency_ms,
            "requires_review": not accepted,
            "fallback_from": {
                "method": rule_result["method"],
                "document_type": rule_result["document_type"],
                "confidence": rule_result["confidence"],
            },
        }


@lru_cache(maxsize=1)
def classification_service() -> ClassificationService:
    return ClassificationService.from_env()


def _float_env(name: str, default: float, minimum: float, maximum: float) -> float:
    raw = os.getenv(name, "").strip()
    if not raw:
        return default
    try:
        value = float(raw)
    except ValueError:
        return default
    return max(minimum, min(maximum, value))


def _int_env(name: str, default: int, minimum: int, maximum: int) -> int:
    raw = os.getenv(name, "").strip()
    if not raw:
        return default
    try:
        value = int(raw)
    except ValueError:
        return default
    return max(minimum, min(maximum, value))
