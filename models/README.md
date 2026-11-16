# Local document-classification models

Model binaries are intentionally not committed to Git.

Download the default ModernBERT INT8 zero-shot classifier:

```bash
python scripts/models/download_modernbert.py
```

This creates:

```text
models/modernbert/
  config.json
  tokenizer.json
  tokenizer_config.json
  special_tokens_map.json
  manifest.json
  onnx/model_int8.onnx
```

Enable it before starting the stack:

```bash
DOCUMENT_CLASSIFIER_BACKEND=modernbert docker compose up -d --build
```

On Windows PowerShell:

```powershell
$env:DOCUMENT_CLASSIFIER_BACKEND="modernbert"
docker compose up -d --build
```

The AI-engine readiness endpoint becomes unhealthy if ModernBERT is explicitly enabled but the local model files cannot be loaded. With the default `rules` backend, no model download is required.
