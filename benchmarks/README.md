# Benchmark and quality suite

The benchmark layer has two purposes:

1. deterministic quality gates for canonical extraction and reconciliation metrics;
2. deployment-level latency measurement against a real API and real document corpus.

## CI quality smoke

```bash
python benchmarks/evaluate.py benchmarks/fixtures/golden_smoke.json \
  --min-field-accuracy 0.95 \
  --min-line-accuracy 0.95 \
  --min-reconciliation-f1 0.95
```

The bundled fixture is intentionally synthetic. It validates the metric implementation and CI gate. It is **not** a claim about production OCR accuracy.

For production evaluation, create a private corpus with licensed/internal invoices and produce an evaluation JSON with the same shape.

Recommended tracked metrics:

- canonical field exact/normalized accuracy
- line-item cell accuracy
- table row/column accuracy
- reconciliation precision
- reconciliation recall
- reconciliation F1
- review-required rate
- false-auto-match rate

False auto-matches should be treated as the highest-cost reconciliation error.

## API latency benchmark

Install the AI-engine environment or otherwise provide `httpx`, then run:

```bash
python benchmarks/run_api_benchmark.py \
  --base-url http://localhost:8080 \
  --input-dir ./private-benchmark-documents \
  --api-key "$RPD_API_KEY"
```

The script reports success rate and mean/p50/p95/max end-to-end document latency.

Keep private/customer documents out of the repository.


## Classification accuracy evaluation

The classification evaluator consumes a JSONL or JSON corpus and reports:

- overall accuracy;
- per-class precision, recall and F1;
- macro precision/recall/F1;
- a three-way invoice / purchase-order / other rollup;
- confusion matrix;
- review-required rate and automatic coverage;
- accuracy on automatically accepted documents;
- classifier method counts;
- p50/p95 classification latency.

The committed `classification_smoke.jsonl` corpus is synthetic and exists only to regression-test the metric and deterministic rules. It is not production evidence.

Private corpus row format:

```json
{"id":"supplier-a-001","expected_type":"invoice","text":"OCR or embedded text here"}
```

For multi-page material, use `text_pages` or the normalized `pages` / `ocr_blocks` contract. Keep customer documents and OCR snapshots outside Git.

Rules regression:

```bash
python benchmarks/classification_evaluate.py \
  benchmarks/fixtures/classification_smoke.jsonl \
  --backend rules \
  --min-accuracy 1.0 \
  --min-macro-f1 1.0
```

ModernBERT CPU evaluation on the private labeled corpus:

```bash
python benchmarks/classification_evaluate.py \
  ./private-benchmark/classification.jsonl \
  --backend modernbert \
  --model-dir models/modernbert \
  --provider cpu \
  --output reports/classification-cpu.json
```

RTX 4060 evaluation uses the same command with a CUDA-enabled ONNX Runtime environment:

```bash
python benchmarks/classification_evaluate.py \
  ./private-benchmark/classification.jsonl \
  --backend modernbert \
  --model-dir models/modernbert \
  --provider cuda \
  --output reports/classification-rtx4060.json
```

Run the latency harness against the same corpus so the report also shows how many calls stayed on rules versus reached ModernBERT:

```bash
python benchmarks/classification_latency.py \
  --backend modernbert \
  --model-dir models/modernbert \
  --provider cpu \
  --corpus ./private-benchmark/classification.jsonl \
  --iterations 100 \
  --output reports/classifier-cpu-latency.json
```

Repeat with `--provider cuda` on the RTX 4060. Do not compare CPU and CUDA runs unless they use the same model revision, max length, corpus and thresholds.

## Full pipeline stage latency

`run_api_benchmark.py` now reads persisted job-step timestamps and reports p50/p95 for:

```text
PREPROCESS
OCR
CLASSIFY
TABLE
MAPPING
VALIDATION
FINALIZE
TOTAL wall time
```

Run the same private document directory several times for a stable sample:

```bash
python benchmarks/run_api_benchmark.py \
  --base-url http://localhost:8080 \
  --input-dir ./private-benchmark-documents \
  --api-key "$RPD_API_KEY" \
  --repeat 3 \
  --output reports/pipeline-cpu.json
```

For a fair CPU-vs-GPU comparison, restart the deployed stack with the intended classifier provider and repeat the identical command/corpus. The report records pipeline timing, but hardware utilization should be captured separately with the host's CPU/GPU monitoring tools.
