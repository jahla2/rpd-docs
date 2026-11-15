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
