from __future__ import annotations

import json
import time
from pathlib import Path
from typing import Any


LABEL_DESCRIPTIONS: dict[str, str] = {
    "invoice": "supplier invoice requesting payment for goods or services",
    "purchase_order": "purchase order authorizing the purchase of goods or services",
    "credit_note": "credit note or credit memo reducing an amount owed",
    "debit_note": "debit note or debit memo increasing or adjusting an amount owed",
    "receipt": "receipt confirming that a payment was received",
    "bank_statement": "bank account statement listing transactions and balances",
    "supplier_statement": "supplier statement or statement of account listing invoices, payments, and balances",
    "payment_advice": "payment or remittance advice explaining a payment",
    "delivery_receipt": "delivery receipt confirming delivery of goods",
    "goods_received_note": "goods received note confirming receipt of purchased goods",
    "erp_export": "export from an ERP or accounting system containing transaction records",
    "spreadsheet": "business spreadsheet or worksheet containing tabular records",
}


class ModernBERTZeroShotClassifier:
    """Warm ONNX Runtime adapter for ModernBERT zero-shot classification.

    The referenced model is an entailment classifier. A document is paired
    with one short hypothesis per supported type and all pairs are evaluated in
    one batch. The session and tokenizer are intentionally loaded once.
    """

    def __init__(
        self,
        model_dir: str,
        *,
        model_file: str = "onnx/model_int8.onnx",
        max_length: int = 256,
        provider: str = "cpu",
        model_version: str = "modernbert-base-zeroshot-v2.0-int8",
    ) -> None:
        try:
            import numpy as np
            import onnxruntime as ort
            from tokenizers import Tokenizer
        except ImportError as exc:
            raise RuntimeError(
                "ModernBERT classification requires numpy, onnxruntime and tokenizers"
            ) from exc

        root = Path(model_dir)
        tokenizer_path = root / "tokenizer.json"
        config_path = root / "config.json"
        onnx_path = root / model_file
        missing = [str(path) for path in (tokenizer_path, config_path, onnx_path) if not path.is_file()]
        if missing:
            raise RuntimeError("ModernBERT model assets are missing: " + ", ".join(missing))

        config = json.loads(config_path.read_text(encoding="utf-8"))
        label2id = config.get("label2id") or {}
        entailment_id = label2id.get("entailment")
        if entailment_id is None:
            entailment_id = 0
        self._entailment_id = int(entailment_id)
        self._np = np
        self._ort = ort
        self._tokenizer = Tokenizer.from_file(str(tokenizer_path))
        self._max_length = max(64, min(int(max_length), 2048))
        self._tokenizer.enable_truncation(max_length=self._max_length)
        self._tokenizer.enable_padding(
            pad_id=int(config.get("pad_token_id", 0)),
            pad_token="<|pad|>",
        )

        available = set(ort.get_available_providers())
        requested = provider.strip().lower()
        if requested == "cuda" and "CUDAExecutionProvider" not in available:
            raise RuntimeError("CUDAExecutionProvider requested but not available")
        if requested in {"cuda", "auto"} and "CUDAExecutionProvider" in available:
            providers = ["CUDAExecutionProvider", "CPUExecutionProvider"]
        else:
            providers = ["CPUExecutionProvider"]

        options = ort.SessionOptions()
        options.graph_optimization_level = ort.GraphOptimizationLevel.ORT_ENABLE_ALL
        options.execution_mode = ort.ExecutionMode.ORT_SEQUENTIAL
        self._session = ort.InferenceSession(str(onnx_path), sess_options=options, providers=providers)
        self._input_names = {entry.name for entry in self._session.get_inputs()}
        self.model = "MoritzLaurer/ModernBERT-base-zeroshot-v2.0"
        self.model_version = model_version
        self.provider = self._session.get_providers()[0] if self._session.get_providers() else "unknown"

    def health(self) -> dict[str, Any]:
        return {
            "ready": True,
            "model": self.model,
            "model_version": self.model_version,
            "provider": self.provider,
            "max_length": self._max_length,
        }

    def classify(self, text: str) -> dict[str, Any]:
        if not text.strip():
            return {
                "document_type": "unknown",
                "confidence": 0.0,
                "alternatives": [],
                "latency_ms": 0,
                "model": self.model,
                "model_version": self.model_version,
                "provider": self.provider,
            }

        started = time.perf_counter_ns()
        labels = list(LABEL_DESCRIPTIONS)
        pairs = [
            (text, f"This document is a {LABEL_DESCRIPTIONS[label]}.")
            for label in labels
        ]
        encodings = self._tokenizer.encode_batch(pairs)

        payload: dict[str, Any] = {}
        if "input_ids" in self._input_names:
            payload["input_ids"] = self._np.asarray([item.ids for item in encodings], dtype=self._np.int64)
        if "attention_mask" in self._input_names:
            payload["attention_mask"] = self._np.asarray(
                [item.attention_mask for item in encodings], dtype=self._np.int64
            )
        if "token_type_ids" in self._input_names:
            payload["token_type_ids"] = self._np.asarray(
                [item.type_ids for item in encodings], dtype=self._np.int64
            )

        outputs = self._session.run(None, payload)
        if not outputs:
            raise RuntimeError("ModernBERT ONNX session returned no outputs")
        logits = self._np.asarray(outputs[0])
        if logits.ndim != 2 or logits.shape[0] != len(labels):
            raise RuntimeError(f"unexpected ModernBERT logits shape {tuple(logits.shape)}")
        if self._entailment_id < 0 or self._entailment_id >= logits.shape[1]:
            raise RuntimeError("ModernBERT entailment label index is outside output logits")

        entailment = logits[:, self._entailment_id].astype(self._np.float64)
        entailment -= entailment.max()
        scores = self._np.exp(entailment)
        scores /= scores.sum()

        order = self._np.argsort(-scores)
        alternatives = [
            {"type": labels[int(index)], "confidence": round(float(scores[int(index)]), 5)}
            for index in order[:3]
        ]
        best = alternatives[0] if alternatives else {"type": "unknown", "confidence": 0.0}
        latency_ms = max(0, (time.perf_counter_ns() - started) // 1_000_000)
        return {
            "document_type": best["type"],
            "confidence": best["confidence"],
            "alternatives": alternatives,
            "latency_ms": latency_ms,
            "model": self.model,
            "model_version": self.model_version,
            "provider": self.provider,
        }
