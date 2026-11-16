# RPD Docs — Document Intelligence Core

A modular document intelligence and reconciliation platform built with **Go + C++ + Python + PostgreSQL**.

## MVP flow

```text
Upload PDF/Image
→ store immutable original
→ C++ preprocessing
→ searchable-PDF text or RapidOCR
→ fast document classification from existing OCR/text JSON
→ type routing (invoice / supplier statement / bank statement)
→ schema-driven financial extraction using OCR text, bounding boxes and table HTML
→ unsupported types go to review, never invoice-by-default
→ header detection + configurable column mapping
→ canonical JSON + searchable PostgreSQL records
→ ERP CSV/XLSX import
→ reconciliation
→ exception review
→ JSON/CSV/XLSX export
```

## Runtime ownership

- **Go API** — intake, tenancy, authentication, mappings, datasets, review, export, audit, durable outbox.
- **Go orchestrator** — retry-safe document processing and deterministic validation.
- **Go reconciliation worker** — queued invoice-to-ERP reconciliation.
- **C++** — PDF/image preprocessing with Poppler + OpenCV.
- **Python** — RapidOCR, fast deterministic document classification, RapidTable/SLANet, extraction and guarded agentic fallback.
- **PostgreSQL** — authoritative jobs, steps, canonical searchable fields, reconciliation and audit.
- **NATS JetStream** — durable processing messages with deduplication.
- **Filesystem/S3-compatible storage** — immutable originals and generated artifacts.

## Complete setup guide

For prerequisites, service wiring, ports, startup, health checks, API smoke tests, ModernBERT setup, logs, reset procedures and troubleshooting, see:

`docs/setup/local-development.md`

## Run the full local MVP

```bash
cp .env.example .env
docker compose up -d --build
```

The Compose stack starts:

```text
postgres
migrate
nats
ai-engine
api
orchestrator
recon-worker
```

Check readiness:

```bash
curl http://localhost:8080/health/ready
```

The API is available at `http://localhost:8080` and the AI engine at `http://localhost:8000`.

Stop the stack:

```bash
docker compose down
```

Or use:

```bash
make up
make logs
make down
```

## Supported financial extraction

The current dynamic extraction router supports:

- `invoice` — existing invoice fields/line items; successful documents become `READY_FOR_RECONCILIATION`
- `supplier_statement` — supplier/date/currency/balances plus transaction rows; successful documents become `COMPLETED`
- `bank_statement` — institution/account/period/currency/balances plus transaction rows; successful documents become `COMPLETED`

Statements use the same OCR and RapidTable outputs already produced by the pipeline. Key fields retain OCR page/block/bounding-box evidence. Other classified document types remain safely routed to review until a schema is implemented.

## Local document classification

The hot path is a cascade:

```text
explicit type
→ deterministic rules
→ ModernBERT ONNX only when rules are uncertain
→ unknown/review when confidence is still insufficient
```

Rules are enabled by default and require no additional model download. To install the optional local ModernBERT INT8 fallback:

```bash
make model-modernbert
```

Then enable it:

```bash
DOCUMENT_CLASSIFIER_BACKEND=modernbert docker compose up -d --build
```

The model is kept outside Git and mounted read-only at `/models`. AI readiness fails if `modernbert` is explicitly enabled but the model cannot be loaded. Classification is capped at three seconds by the Go orchestrator; a timeout becomes `unknown` and routes to review instead of blocking the pipeline.

Benchmark the local classifier after installing the model:

```bash
python benchmarks/classification_latency.py --backend modernbert --provider cpu
```

The default ModernBERT adapter uses the INT8 ONNX graph with a 256-token cap to prioritize latency. Common high-confidence invoices and purchase orders remain on the deterministic path and never invoke the model.

## Run tests

Component tests:

```bash
make test
```

Full API-level smoke after the Compose stack is ready:

```bash
pip install "requests>=2.32,<3" "reportlab>=4.2,<5"
make e2e
```

The E2E smoke validates upload → canonical extraction → duplicate detection → reprocess → ERP import → reconciliation → export.

## Reliability model

- immutable original documents
- unique `run_id` per processing attempt
- DB-backed job steps and attempt history
- idempotent document version creation per run
- JetStream message IDs for publisher-side deduplication
- worker progress heartbeats during long processing
- transactional outbox for document and reconciliation queue publication
- canonical fields, line items, validation results, OCR provenance and document-classification provenance persisted in PostgreSQL
- local and S3-compatible artifact reads
- resumable SSE status streams
- deterministic validation before reconciliation

See [docs/architecture/document-intelligence-core-guide.md](docs/architecture/document-intelligence-core-guide.md).
