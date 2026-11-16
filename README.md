# RPD Docs — Document Intelligence Core

A modular document intelligence and reconciliation platform built with **Go + C++ + Python + PostgreSQL**.

## MVP flow

```text
Upload PDF/Image
→ store immutable original
→ C++ preprocessing
→ searchable-PDF text or RapidOCR
→ fast document classification from existing OCR/text JSON
→ type routing (unsupported extractors go to review, never invoice-by-default)
→ RapidTable/SLANet only for routes that need table structure
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
