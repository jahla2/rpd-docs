# Document Intelligence Core — Implementation Guide

**Version:** 1.0  
**Languages:** Go, C++, Python  
**Database:** PostgreSQL  
**OCR:** RapidOCR  
**Table Recognition:** RapidTable / lightweight Paddle SLANet  
**Status:** Source of truth for the MVP

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

Supported transforms:

```text
rename
normalize
date conversion
decimal conversion
split
combine
compute
lookup
default
exclude
```

Never execute arbitrary user scripts. Mapping operations come from an allowlisted transform registry.

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

### Reconciliation
- `POST /api/v1/reconciliation-runs`
- `GET /api/v1/reconciliation-runs/{id}`
- `GET /api/v1/reconciliation-runs/{id}/matches`
- `GET /api/v1/reconciliation-runs/{id}/exceptions`
- `POST /api/v1/reconciliation-runs/{id}/matches/{matchId}/approve`
- `POST /api/v1/reconciliation-runs/{id}/matches/{matchId}/reject`

### Review / export
- `GET /api/v1/review-tasks`
- `POST /api/v1/review-tasks/{id}/resolve`
- `POST /api/v1/exports`
- `GET /api/v1/exports/{id}`

### Health
- `GET /health/live`
- `GET /health/ready`

## 15. MVP

MVP must support: one tenant, PDF/image intake, CSV/XLSX ERP import, searchable-PDF fast path, C++ preprocessing, RapidOCR, RapidTable/SLANet, invoice canonical schema, line items, header aliases, mapping preview, required-field/total validation, supplier normalization, exact/tolerance reconciliation, duplicate detection, review tasks, JSON/CSV/XLSX exports, audit, job status, SSE, Docker local dependencies and integration tests.

Non-goals for MVP: Kubernetes, dozens of microservices, fully autonomous financial approval, general-purpose scripting, custom model training and 20 OCR engines.

## 16. Implementation checklist

### Phase 0 — repo
- [x] monorepo
- [x] Go workspace
- [x] Python project
- [x] CMake project
- [x] env example
- [x] Docker Compose dependencies
- [x] architecture guide
- [x] versioned JSON contracts
- [ ] CI green on implementation branch

### Phase 1 — foundation
- [x] PostgreSQL core migration
- [x] tenant/document/job/artifact core tables
- [x] storage layout abstraction
- [x] queue contract
- [x] structured service configuration
- [x] liveness/readiness endpoints
- [ ] production auth/API keys
- [ ] object storage adapter

### Phase 2 — intake/native
- [x] upload endpoint skeleton
- [x] SHA-256 and immutable storage path
- [x] document/job creation
- [x] publish preprocess job
- [x] native-engine contract/CLI
- [ ] full PDF rasterization
- [ ] OpenCV deskew/denoise/quality implementation

### Phase 3 — OCR/table
- [x] Python service shell
- [x] RapidOCR adapter
- [x] normalized OCR contract
- [x] RapidTable/SLANet adapter shell
- [ ] production OCR fixtures and accuracy benchmarks
- [ ] table-region detector

### Phase 4 — mapping/canonical
- [x] deterministic header normalization
- [x] invoice alias registry
- [x] data-type inferencer
- [x] mapping preview API
- [x] canonical invoice extractor
- [x] OCR-guided table-region detector
- [x] deterministic line-item header mapping
- [ ] persisted mapping-template CRUD/version activation
- [ ] richer column profile statistics
- [ ] complete financial subtotal/tax validation rules

### Phase 5 — ERP/reconciliation
- [x] dataset creation/query APIs
- [x] CSV parser
- [x] XLSX parser
- [x] automatic ERP header detection/mapping
- [x] optional header mapping overrides
- [x] ERP canonicalization
- [x] decimal-safe amount normalization
- [x] row validation
- [x] dataset-local duplicate row detection
- [x] indexed candidate generator
- [x] exact match
- [x] composite scoring
- [x] amount tolerance
- [x] date window
- [x] ambiguous/equal-score duplicate detection
- [x] persisted match evidence
- [x] reconciliation exceptions
- [x] reviewer approve/reject endpoints
- [x] review-task resolution with audit event
- [ ] fuzzy supplier similarity beyond normalized exact matching
- [ ] async reconciliation for large batches
- [ ] persisted original ERP import artifact/provenance

### Phase 6 — export/AI
- [x] JSON reconciliation export
- [x] CSV reconciliation export
- [x] XLSX reconciliation export
- [x] persisted export metadata/download endpoint
- [x] basic review workflow
- [ ] configurable output profiles
- [ ] configurable output column order
- [ ] selective agent fallback
- [ ] structured agent evidence
- [ ] full field/line-item correction workflow

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
