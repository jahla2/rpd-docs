# Document Intelligence Core — Implementation Guide

**Version:** 2.0  
**Languages:** Go, C++, Python  
**Database:** PostgreSQL  
**OCR:** RapidOCR  
**Table Recognition:** RapidTable / lightweight Paddle SLANet  
**Status:** Source of truth for the implemented MVP core

## 1. Goal

Build a modular platform that accepts PDFs, images, CSV and XLSX; extracts text and tables; detects headers; maps source columns to canonical fields; validates financial data; reconciles canonical records against ERP/reference data; routes uncertain cases to review; and exports JSON/CSV/XLSX.

The first complete workflow is:

```text
Upload scanned invoice PDF
→ immutable storage
→ C++ preprocessing
→ RapidOCR
→ RapidTable/SLANet
→ header detection
→ configurable mapping
→ canonical invoice JSON
→ ERP import
→ reconciliation
→ review
→ export
```

## 2. Runtime ownership

### Go — control plane

Go owns HTTP APIs, tenancy, document intake, job state, queueing, mapping configuration, deterministic normalization/validation, reconciliation, export, audit and health checks.

### C++ — native processing

C++ owns measured performance-sensitive work: PDF/image preprocessing, rotation, deskew, denoise, resize, quality scoring, native parsers and future hotspots. Do not rewrite business logic in C++ unless profiling justifies it.

### Python — document intelligence

Python owns RapidOCR, RapidTable/SLANet, layout reconstruction, header detection experiments, semantic mapping suggestions, embeddings and agentic fallback. AI can suggest; deterministic validation remains authoritative.

## 3. Core principles

1. Process once and reuse OCR/table/layout artifacts.
2. Do not OCR CSV/XLSX.
3. Searchable PDFs use embedded text when quality is sufficient.
4. Every important field keeps provenance: page, bbox, raw text, normalized value, method, confidence and model version.
5. Version pipeline, OCR model, table model, mapping template, reconciliation rules and prompts.
6. Reconciliation works on canonical data, never directly on raw OCR strings.
7. Financial values use fixed-precision decimals, not binary floats.
8. Large artifacts live in file/object storage; PostgreSQL stores authoritative metadata and searchable canonical data.
9. Workers are idempotent because queue delivery may repeat.
10. Agentic AI is a selective fallback, not the default path.

## 4. High-level architecture

```text
Frontend
   │
   ▼
Go API ───────► PostgreSQL
   │
   ├──────────► Storage
   │
   └──────────► NATS/Queue
                    │
                    ▼
              Go Orchestrator
                │         │
                ▼         ▼
              C++       Python
           preprocess   OCR/tables/AI
                │         │
                └────┬────┘
                     ▼
              Canonical document
                     │
                     ▼
             Go reconciliation
                │         │
                ▼         ▼
              Match     Review
                └────┬────┘
                     ▼
                   Export
```

## 5. Processing states

```text
RECEIVED
QUEUED
CLASSIFIED
PREPROCESSING
PREPROCESSED
OCR_PROCESSING
OCR_COMPLETED
TABLE_PROCESSING
TABLES_COMPLETED
LAYOUT_PROCESSING
LAYOUT_COMPLETED
MAPPING
MAPPED
VALIDATING
VALIDATED
REVIEW_REQUIRED
READY_FOR_RECONCILIATION
RECONCILING
RECONCILED
EXPORTING
COMPLETED
FAILED
CANCELLED
```

Non-table documents may skip the table states.

## 6. OCR pipeline

### Intake

Go creates `document_id`, `job_id`, `run_id`, calculates SHA-256, validates MIME/size, stores the immutable original and publishes the first job.

### Routing

- Searchable PDF → embedded text path when sufficient.
- Scanned PDF → rasterization → preprocessing → OCR.
- PNG/JPEG/TIFF → preprocessing → OCR.
- CSV/XLSX → native parser → header detection → mapping.

### C++ preprocessing

Planned modules: PDF rendering, image decode, auto-rotate, deskew, resize, denoise, contrast normalization, thresholding when useful, blank-page detection and page-quality scoring.

### RapidOCR

Python exposes a stable adapter that initializes the model once and converts engine output to a common contract:

```json
{
  "schema_version": "1.0",
  "document_id": "doc",
  "page": 1,
  "engine": "rapidocr",
  "blocks": [
    {
      "block_id": "b001",
      "text": "Invoice Number: INV-001",
      "bbox": [120, 85, 550, 119],
      "confidence": 0.98
    }
  ]
}
```

### Table recognition

Use RapidTable/SLANet for detected table regions. Reuse existing OCR boxes/text when possible instead of rerunning full-page OCR. Normalize output to rows, cells, row/col spans and bounding boxes.

### Layout

Combine OCR blocks, table cells, reading order, headers, footers and sections. Persist `layout.json`, `reading_order.json` and `document.md`.

## 7. Canonical invoice schema

Initial document type: invoice.

Header fields:

```text
supplier_name
supplier_code
invoice_number
invoice_date
due_date
po_number
currency
subtotal
tax
grand_total
```

Line items:

```text
description
item_code
quantity
unit
unit_price
tax
line_total
```

## 8. Column Header Definition and Mapping Engine

Different suppliers can use different labels for the same business meaning.

| Source | Alternate | ERP | Canonical |
|---|---|---|---|
| Invoice No | Inv Ref | Reference | invoice_number |
| Item Desc | Particulars | Product | description |
| Qty | Quantity | Units | quantity |
| Unit Cost | Price Each | Unit Rate | unit_price |
| Total | Extended Amount | Line Amount | line_total |

Modules:

- **Header Row Detector** — identifies the row containing headers.
- **Header Normalizer** — case/space/punctuation normalization.
- **Alias Dictionary** — tenant-configurable aliases such as Qty/Units/Quantity.
- **Data Type Inferencer** — string, decimal, date, currency, identifier, etc.
- **Column Profile Analyzer** — null %, uniqueness, numeric/date ratios, regex shape.
- **Semantic Column Matcher** — Python embeddings/LLM suggestion for ambiguous labels.
- **Template Registry** — versioned supplier/document mappings.
- **Mapping Validator** — required fields, duplicate targets, type compatibility.
- **Output Schema Builder** — selected output fields and ordering.

Implemented transforms:

```text
normalize
date
decimal
default
exclude
```

Unsupported transforms are rejected rather than ignored. Future transforms such as split/combine/compute/lookup require an explicit runtime implementation and tests before being added to the allowlist. Never execute arbitrary user scripts.

Example template:

```json
{
  "template_id": "supplier_invoice_v1",
  "version": 1,
  "document_type": "invoice",
  "header_row": "auto",
  "columns": [
    {
      "source_aliases": ["Inv Ref", "Invoice No"],
      "target": "invoice_number",
      "type": "string",
      "required": true
    }
  ],
  "output": {
    "format": "xlsx",
    "columns": ["invoice_number", "line_total"]
  }
}
```

The exact template version used is persisted with every processing version.

## 9. Agentic fallback

Trigger only when required fields are missing, OCR confidence is low, table/header interpretation is ambiguous, totals fail validation, supplier resolution fails, document type is unknown, or reconciliation has multiple close candidates.

Agent output must be structured, cite source block IDs/crops and pass deterministic validation before becoming canonical.

## 10. Reconciliation pipeline

```text
Canonical document
      +
ERP/reference dataset
      │
      ▼
Normalization
      │
      ▼
Indexed candidate generation
      │
      ▼
Exact match
      │
      ▼
Composite / tolerance / date-window
      │
      ▼
Fuzzy and grouped matching
      │
      ▼
Validation
  ┌───┼───────────────┐
  ▼   ▼               ▼
Match Suggested     Exception
```

### Modules

- source/reference normalization
- candidate generation using supplier, currency, date window, amount range and reference indexes
- exact supplier + invoice matching
- composite scoring
- amount tolerance
- date window
- fuzzy supplier matching
- duplicate detection
- one-to-many
- many-to-one
- future PO/GRN/invoice three-way matching

Recommended statuses:

```text
MATCHED_EXACT
MATCHED_TOLERANCE
MATCHED_COMPOSITE
MATCHED_ONE_TO_MANY
MATCHED_MANY_TO_ONE
SUGGESTED_MATCH
UNMATCHED
DUPLICATE
CONFLICT
REVIEW_REQUIRED
REJECTED
APPROVED
```

## 11. Storage layout after processing

```text
storage/
└── tenants/{tenant_id}/
    ├── inbound/{yyyy}/{mm}/{document_id}/
    │   ├── original.ext
    │   └── upload_manifest.json
    ├── processing/{document_id}/{run_id}/
    │   ├── preprocess/
    │   │   ├── page-001.png
    │   │   └── quality.json
    │   ├── ocr/
    │   │   └── page-001.json
    │   ├── tables/
    │   │   ├── table-001.json
    │   │   └── table-001.html
    │   ├── layout/
    │   │   ├── blocks.json
    │   │   ├── reading_order.json
    │   │   └── document.md
    │   └── extraction/
    │       ├── candidates.json
    │       ├── mapped_fields.json
    │       └── agent_trace.json
    ├── documents/{document_id}/{version_id}/
    │   ├── canonical.json
    │   ├── fields.json
    │   ├── line_items.json
    │   ├── provenance.json
    │   └── validation.json
    ├── reconciliation/{reconciliation_id}/
    │   ├── matches.json
    │   ├── unmatched.json
    │   ├── exceptions.json
    │   └── report.json
    └── exports/{export_id}/
        ├── result.json
        ├── result.csv
        └── result.xlsx
```

Originals are immutable. Every retry gets a new `run_id`. Validated reprocessing creates a new document version.

## 12. Source repository structure

```text
apps/
  api/              Go HTTP API
  orchestrator/     Go workers
  native-engine/    C++ processing
  ai-engine/        Python OCR/table/AI
packages/
  contracts/
  canonical-schema/
  mapping-templates/
database/
  migrations/
infrastructure/
tests/
docs/
```

## 13. PostgreSQL design

Core domains:

- tenancy: `tenants`, users/api keys later
- documents: `documents`, `document_versions`, `artifacts`
- jobs: `processing_jobs`, `job_steps`
- extraction: `extracted_fields`, `document_line_items`, `validation_results`
- configuration: `document_types`, `field_definitions`, `mapping_templates`
- reference data: `suppliers`, `supplier_aliases`, `erp_datasets`, `erp_records`
- reconciliation: `reconciliation_runs`, `matches`, `reconciliation_exceptions`
- governance: `review_tasks`, `audit_events`, `model_registry`

Every tenant-owned table is tenant-scoped. Index candidate fields before fuzzy processing.

## 14. Core API endpoints

### Documents
- `POST /api/v1/documents`
- `GET /api/v1/documents/{id}`
- `GET /api/v1/documents/{id}/result`
- `GET /api/v1/documents/{id}/artifacts`
- `POST /api/v1/documents/{id}/reprocess`

### Jobs
- `GET /api/v1/jobs/{id}`
- `GET /api/v1/jobs/{id}/steps`
- `GET /api/v1/jobs/{id}/events` (SSE)

### Configuration
- `GET/POST /api/v1/document-types`
- `GET/POST /api/v1/field-definitions`
- `GET/POST /api/v1/mapping-templates`
- `POST /api/v1/mapping-templates/{id}/versions`
- `POST /api/v1/mapping-templates/{id}/preview`
- `POST /api/v1/mapping-templates/{id}/activate`

### Datasets
- `POST /api/v1/datasets`
- `POST /api/v1/datasets/{id}/import`
- `GET /api/v1/datasets/{id}/records`
- `GET /api/v1/datasets/{id}/imports`

### Reconciliation
- `POST /api/v1/reconciliation-runs`
- `GET /api/v1/reconciliation-runs/{id}`
- `POST /api/v1/reconciliation-runs/{id}/cancel`
- `GET /api/v1/reconciliation-runs/{id}/events` (SSE)
- `GET /api/v1/reconciliation-runs/{id}/matches`
- `GET /api/v1/reconciliation-runs/{id}/exceptions`
- `POST /api/v1/reconciliation-runs/{id}/matches/{matchId}/approve`
- `POST /api/v1/reconciliation-runs/{id}/matches/{matchId}/reject`
- `POST /api/v1/reconciliation-runs/{id}/export`

### Review / corrections / export
- `GET /api/v1/review-tasks`
- `GET /api/v1/review-tasks/{id}`
- `POST /api/v1/review-tasks/{id}/resolve`
- `POST /api/v1/documents/{id}/corrections`
- `GET/POST /api/v1/output-profiles`
- `GET /api/v1/output-profiles/{id}`
- `GET /api/v1/exports/{id}`
- `GET /api/v1/exports/{id}/download`

### Suppliers
- `GET /api/v1/suppliers`
- `POST /api/v1/suppliers`
- `POST /api/v1/suppliers/{id}/aliases`

### Artifacts / security
- `GET /api/v1/artifacts/{id}/access`
- `GET /api/v1/artifacts/{id}/content`
- `GET/POST /api/v1/admin/api-keys`
- `POST /api/v1/admin/api-keys/{id}/revoke`

### Health
- `GET /health/live`
- `GET /health/ready`

## 15. MVP

MVP must support: one tenant, PDF/image intake, CSV/XLSX ERP import, searchable-PDF fast path, C++ preprocessing, RapidOCR, RapidTable/SLANet, invoice canonical schema, line items, header aliases, mapping preview, required-field/total validation, supplier normalization, exact/tolerance reconciliation, duplicate detection, review tasks, JSON/CSV/XLSX exports, audit, job status, SSE, Docker local dependencies and integration tests.

Non-goals for MVP: Kubernetes, dozens of microservices, fully autonomous financial approval, general-purpose scripting, custom model training and 20 OCR engines.

## 16. Implementation checklist

### Phase 0 — repository and CI
- [x] monorepo
- [x] Go workspace
- [x] Python project
- [x] CMake project
- [x] environment template
- [x] Docker/local dependency foundation
- [x] architecture guide
- [x] versioned contracts
- [x] Go/Python/C++/PostgreSQL CI
- [x] synthetic quality gate and Go reconciliation microbenchmarks
- [x] S3-compatible integration test
- [ ] private production-quality benchmark corpus

### Phase 1 — platform foundation
- [x] PostgreSQL migrations
- [x] tenant/document/job/artifact tables
- [x] durable NATS contracts
- [x] liveness/readiness
- [x] API-key authentication
- [x] ADMIN/OPERATOR/REVIEWER/READER RBAC
- [x] tenant identity derived from authenticated API key when auth is enabled
- [x] per-key rate limiting
- [x] bootstrap key CLI
- [x] local storage backend
- [x] S3-compatible storage backend
- [x] signed S3 artifact access
- [ ] end-user/JWT login flow if a first-party web UI requires interactive users
- [ ] production secret manager / key rotation automation

### Phase 2 — document intake and native processing
- [x] upload API
- [x] SHA-256 immutable intake
- [x] document/job creation
- [x] multi-page PDF page discovery
- [x] Poppler PDF rasterization
- [x] searchable-PDF embedded-text fast path
- [x] mixed searchable/scanned page routing
- [x] OpenCV deskew/denoise/CLAHE/resize/quality scoring
- [x] blank-page routing
- [x] S3 rematerialization on worker hosts

### Phase 3 — OCR and tables
- [x] RapidOCR adapter
- [x] normalized OCR blocks
- [x] Poppler bbox-layout normalization
- [x] OCR-guided table-region detection
- [x] RapidTable/SLANet+ adapter
- [x] reuse existing OCR/text boxes for table recognition
- [x] multi-page extraction merge
- [ ] private OCR/table accuracy fixtures representing production suppliers

### Phase 4 — mapping, canonicalization and validation
- [x] header normalization
- [x] alias registry
- [x] data-type inference
- [x] mapping preview
- [x] persisted mapping-template CRUD
- [x] template versioning and activation
- [x] exact template version persisted on document version
- [x] runtime mapping aliases for document fields and table columns
- [x] allowlisted transforms
- [x] canonical invoice extraction
- [x] line-item extraction
- [x] required-field validation
- [x] quantity × unit-price validation
- [x] line-sum/subtotal validation
- [x] subtotal/tax/grand-total validation
- [x] validation evidence artifact
- [x] richer configurable column profiling statistics

### Phase 5 — ERP and reconciliation
- [x] dataset APIs
- [x] CSV import
- [x] XLSX import
- [x] ERP header mapping and overrides
- [x] decimal-safe canonicalization
- [x] duplicate ERP row hashing
- [x] indexed candidate generation
- [x] exact/tolerance/composite matching
- [x] fuzzy supplier/master-data resolution
- [x] supplier aliases
- [x] one invoice → many ERP rows
- [x] many invoices → one ERP row
- [x] same-run ERP target exclusivity
- [x] ambiguous/equal-score review routing
- [x] async queued reconciliation worker
- [x] run progress/events/cancellation
- [x] grouped-match persistence and export visibility
- [x] persisted original ERP import file/provenance artifact
- [ ] PO/GRN/invoice three-way matching (post-MVP)

### Phase 6 — review, agentic fallback and export
- [x] review task APIs
- [x] approve/reject reconciliation matches
- [x] audited field corrections
- [x] audited line-item replacement
- [x] immutable corrected document versions
- [x] deterministic revalidation after correction
- [x] selective agentic fallback
- [x] Ollama agent provider
- [x] OpenAI-compatible agent provider
- [x] strict agent field allowlist
- [x] agent evidence/model/prompt tracking
- [x] deterministic validation after every agent proposal
- [x] JSON/CSV/XLSX reconciliation exports
- [x] configurable output profiles
- [x] configurable output column order/header aliases
- [x] local or S3-backed export download

### Phase 7 — quality and production hardening
- [x] durable SSE document events
- [x] durable SSE reconciliation events
- [x] resumable SSE with Last-Event-ID
- [x] synthetic golden field/line/reconciliation metric evaluator
- [x] reconciliation precision/recall/F1 calculation
- [x] p50/p95 deployed-API latency benchmark harness
- [x] Go reconciliation microbenchmarks
- [x] full migration-chain CI
- [x] real two-page searchable-PDF native smoke test
- [x] S3-compatible upload/read/presign integration test
- [ ] private representative invoice/ERP benchmark corpus
- [ ] sustained concurrency/load test in a production-like environment
- [ ] backup/restore and retention-policy validation
- [ ] external security review / penetration testing before public exposure

## 17. Development order

Build vertical slices:

1. Upload → storage → queue → job status.
2. Preprocess → OCR JSON.
3. OCR → table → canonical invoice.
4. Mapping → validation.
5. ERP import → exact reconciliation.
6. Tolerance → review → export.
7. Agentic fallback.

## 18. Definition of MVP done

A scanned invoice can be uploaded, stored immutably, preprocessed, OCR'd, table-extracted, mapped to canonical invoice JSON, validated, reconciled against imported ERP data, reviewed when uncertain, and exported with an auditable evidence trail.

## 19. Quality and observability

Track p50/p95 latency, pages/sec, OCR accuracy, table-cell accuracy, mapping accuracy, reconciliation precision/recall, review rate, CPU/RAM/GPU, errors per stage and agent fallback rate. Use structured logs containing tenant/document/job/run/step IDs.

## 20. Security

Use TLS externally, tenant scoping, hashed API keys, MIME/size validation, safe storage IDs, RBAC, audit events, secret management, log redaction and encrypted storage where available. Never execute arbitrary mapping scripts.
