# RPD Docs — Document Intelligence Core

A modular document intelligence and reconciliation platform built with **Go + C++ + Python + PostgreSQL**.

## MVP flow

```text
Upload PDF/Image
→ store immutable original
→ C++ preprocessing
→ RapidOCR
→ RapidTable/SLANet table recognition
→ header detection + configurable column mapping
→ canonical JSON
→ ERP CSV/XLSX import
→ reconciliation
→ exception review
→ JSON/CSV/XLSX export
```

## Runtime ownership

- **Go** — API, tenancy, orchestration, queues, mappings, validation, reconciliation, exports, audit.
- **C++** — native document/image preprocessing and measured performance hotspots.
- **Python** — RapidOCR, RapidTable/SLANet, layout intelligence, semantic mapping, agentic fallback.
- **PostgreSQL** — authoritative metadata, jobs, canonical searchable fields, configuration, reconciliation and audit.
- **Filesystem/object storage** — original documents and generated artifacts.

## Start local dependencies

```bash
docker compose up -d postgres nats
```

Apply `database/migrations/0001_core.sql`, then run the services described in the architecture guide.

See [docs/architecture/document-intelligence-core-guide.md](docs/architecture/document-intelligence-core-guide.md).
