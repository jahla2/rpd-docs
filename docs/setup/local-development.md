# RPD Docs — Complete Local Setup and Operations Guide

This guide is the source of truth for running the current RPD Docs MVP locally.

It covers the Docker Compose stack, service connections, ports, optional ModernBERT classification, health checks, API smoke tests, logs, reset procedures, and common startup problems.

## 1. Current local architecture

```text
Client / Browser / API Tool
          |
          | HTTP :8080
          v
+-----------------------+
| Go API                |
| rpd-api               |
+----+-------------+----+
     |             |
     |             +------------------------------+
     |                                            |
     | PostgreSQL :5432                          | NATS JetStream :4222
     |                                            |
     v                                            v
+-------------+                          +------------------+
| PostgreSQL  |                          | NATS JetStream   |
+-------------+                          +--------+---------+
                                                   |
                                                   v
                                      +-----------------------+
                                      | Go Orchestrator       |
                                      | rpd-orchestrator      |
                                      +-----+-----------+-----+
                                            |           |
                                            |           | HTTP :8000
                                            |           v
                                            |   +------------------+
                                            |   | Python AI Engine |
                                            |   | RapidOCR/Table   |
                                            |   | Classifier       |
                                            |   +------------------+
                                            |
                                            | local process call
                                            v
                                      +-----------------------+
                                      | C++ Native Engine     |
                                      | rpd-native-engine     |
                                      | no network port       |
                                      +-----------------------+

NATS also feeds the Go reconciliation worker.
API, orchestrator and reconciliation worker share the artifact storage volume.
```

## 2. Services and ports

| Component | Compose service | Host port | Internal endpoint | Purpose |
| --- | --- | ---: | --- | --- |
| Go API | `api` | 8080 | `api:8080` | Upload, jobs, mappings, datasets, review, exports, health |
| Python AI engine | `ai-engine` | 8000 | `ai-engine:8000` | RapidOCR, PDF text, document classification, tables, extraction |
| PostgreSQL | `postgres` | 5432 | `postgres:5432` | Metadata, jobs, canonical data, reconciliation, audit |
| NATS JetStream | `nats` | 4222 | `nats:4222` | Durable work queue |
| NATS monitoring | `nats` | 8222 | `nats:8222` | NATS monitoring endpoint |
| Go orchestrator | `orchestrator` | none | none | Consumes document jobs and runs the pipeline |
| Go reconciliation worker | `recon-worker` | none | none | Consumes reconciliation jobs |
| DB migrator | `migrate` | none | none | Applies SQL migrations and exits |
| C++ native engine | embedded binary | none | `/usr/local/bin/rpd-native-engine` | PDF/image preprocessing |

The C++ engine is intentionally not another HTTP service. It is compiled into the API/orchestrator images and invoked as a local executable by the Go processing layer.

## 3. How the applications are connected

The default Compose wiring is:

```text
API
  DATABASE_URL -> postgres:5432
  NATS_URL     -> nats:4222
  AI_ENGINE_URL -> http://ai-engine:8000
  NATIVE_ENGINE_PATH -> /usr/local/bin/rpd-native-engine
  STORAGE_ROOT -> /data/storage

Orchestrator
  DATABASE_URL -> postgres:5432
  NATS_URL     -> nats:4222
  AI_ENGINE_URL -> http://ai-engine:8000
  NATIVE_ENGINE_PATH -> /usr/local/bin/rpd-native-engine
  STORAGE_ROOT -> /data/storage

Reconciliation worker
  DATABASE_URL -> postgres:5432
  NATS_URL     -> nats:4222
  STORAGE_ROOT -> /data/storage

AI engine
  STORAGE_ROOT is mounted read-only through the shared Compose volume
  optional model files are mounted read-only from ./models
```

The Go API readiness endpoint checks all required runtime dependencies:

- PostgreSQL
- storage
- NATS
- the C++ native executable
- Python AI engine readiness

If any required dependency is unavailable, `GET /health/ready` returns HTTP 503.

## 4. Prerequisites

### Recommended path: Docker

Install:

- Git
- Docker Desktop or Docker Engine
- Docker Compose v2

For Windows 11, Docker Desktop with the WSL 2 backend is recommended.

Check:

```powershell
git --version
docker --version
docker compose version
```

You do not need to install Go, C++, PostgreSQL, NATS, RapidOCR, or RapidTable on the host when using the full Docker Compose path.

### Optional host-development tools

Only needed when running components outside Docker:

- Go 1.23
- Python 3.11+; CI uses Python 3.12
- CMake
- C++ compiler
- OpenCV development libraries
- Poppler utilities
- PostgreSQL client if desired

## 5. Clone and select the current development branch

```powershell
git clone https://github.com/jahla2/rpd-docs.git
cd rpd-docs
git fetch origin
git checkout feat/mvp-foundation
git pull origin feat/mvp-foundation
```

The active MVP implementation currently lives on `feat/mvp-foundation`. PR #1 targets `main` and remains unmerged until explicitly approved.

## 6. Configure the environment

### Windows PowerShell

```powershell
Copy-Item .env.example .env
```

### Linux/macOS

```bash
cp .env.example .env
```

The default local configuration uses:

```text
AUTH_MODE=disabled
STORAGE_BACKEND=local
DOCUMENT_CLASSIFIER_BACKEND=rules
AGENT_PROVIDER=disabled
AGENT_FALLBACK_ENABLED=false
```

These defaults are appropriate for local development only.

## 7. Validate Compose before startup

```powershell
docker compose config
```

This must complete without an error.

## 8. Start the complete local stack

```powershell
docker compose up -d --build
```

Equivalent Make target:

```powershell
make up
```

The stack starts:

```text
postgres
migrate
nats
ai-engine
api
orchestrator
recon-worker
```

The `migrate` service is expected to exit successfully after applying all SQL migrations.

## 9. Check container status

```powershell
docker compose ps
```

Expected:

- `postgres` running/healthy
- `nats` running
- `ai-engine` running/healthy
- `api` running
- `orchestrator` running
- `recon-worker` running
- `migrate` exited with code 0

## 10. Verify readiness

### Full API readiness

Windows:

```powershell
curl.exe http://localhost:8080/health/ready
```

Expected shape:

```json
{
  "ready": true,
  "checks": {
    "postgres": true,
    "storage": true,
    "nats": true,
    "native_engine": true,
    "ai_engine": true
  }
}
```

### API liveness

```powershell
curl.exe http://localhost:8080/health/live
```

### AI engine readiness

```powershell
curl.exe http://localhost:8000/health/ready
```

### NATS monitoring

Open:

```text
http://localhost:8222/
```

### PostgreSQL

```powershell
docker compose exec postgres pg_isready -U rpd -d rpd
```

## 11. Default document-classification mode

The default mode is the zero-download deterministic fast path:

```text
DOCUMENT_CLASSIFIER_BACKEND=rules
```

This is sufficient to run the complete current MVP.

Pipeline:

```text
Upload
-> C++ preprocess
-> embedded PDF text or RapidOCR
-> CLASSIFY
-> invoice route or review route
-> RapidTable when required
-> extraction
-> mapping
-> validation
-> canonical data
-> reconciliation
-> review/export
```

## 12. Enable local ModernBERT classification

ModernBERT is optional.

Download the local model:

```powershell
python scripts/models/download_modernbert.py
```

or:

```powershell
make model-modernbert
```

Then set in `.env`:

```text
DOCUMENT_CLASSIFIER_BACKEND=modernbert
DOCUMENT_CLASSIFIER_PROVIDER=cpu
```

Recreate the AI engine and processing services:

```powershell
docker compose up -d --build ai-engine api orchestrator
```

If ModernBERT is enabled but its assets cannot be loaded, AI readiness intentionally fails instead of silently pretending the classifier is ready.

The Go orchestrator imposes a three-second document-classification deadline. A timed-out classification becomes `unknown` and is routed to review.

## 13. Upload a document manually

With local authentication disabled:

```powershell
curl.exe -X POST http://localhost:8080/api/v1/documents ^
  -F "file=@C:\path\to\invoice.pdf"
```

In PowerShell, using `Invoke-RestMethod` with multipart support or an API client such as Postman/Bruno is also fine.

The response returns:

- `document_id`
- `job_id`
- `run_id`
- initial status
- SHA-256
- duplicate information

## 14. Inspect processing

Get a job:

```text
GET http://localhost:8080/api/v1/jobs/{jobId}
```

Get stage history:

```text
GET http://localhost:8080/api/v1/jobs/{jobId}/steps
```

Current main processing stages:

```text
PREPROCESS
OCR
CLASSIFY
TABLE
MAPPING
VALIDATION
FINALIZE
```

Get document metadata:

```text
GET http://localhost:8080/api/v1/documents/{documentId}
```

Get canonical output:

```text
GET http://localhost:8080/api/v1/documents/{documentId}/result
```

Get artifacts:

```text
GET http://localhost:8080/api/v1/documents/{documentId}/artifacts
```

## 15. Run the automated end-to-end smoke test

Install only the E2E client dependencies on the host:

```powershell
python -m pip install "requests>=2.32,<3" "reportlab>=4.2,<5"
```

Then:

```powershell
python tests/end-to-end/e2e_smoke.py
```

or:

```powershell
make e2e
```

The smoke test exercises:

```text
searchable invoice PDF
-> upload
-> preprocess
-> text/OCR contract
-> document classification
-> table recognition
-> canonical extraction
-> duplicate detection
-> immutable reprocess
-> ERP dataset/import
-> reconciliation
-> match verification
-> JSON export
-> job-step verification
```

## 16. ERP reconciliation API

The detailed ERP API workflow is documented in:

```text
docs/api/erp-reconciliation-mvp.md
```

High-level sequence:

```text
POST /api/v1/datasets
POST /api/v1/datasets/{datasetId}/import
POST /api/v1/reconciliation-runs
GET  /api/v1/reconciliation-runs/{id}
GET  /api/v1/reconciliation-runs/{id}/matches
GET  /api/v1/reconciliation-runs/{id}/exceptions
POST /api/v1/reconciliation-runs/{id}/export
GET  /api/v1/exports/{id}/download
```

## 17. Important API groups currently available

The Go API currently exposes endpoints for:

- health
- documents and reprocessing
- document artifacts/results
- jobs, pages, steps and SSE events
- mapping templates
- ERP datasets/imports/records
- reconciliation runs, matches, exceptions and events
- approve/reject review actions
- review tasks and corrections
- exports
- suppliers and aliases
- API-key administration
- artifact access/content
- output profiles

The Python AI engine exposes its own FastAPI routes and interactive API documentation at:

```text
http://localhost:8000/docs
```

The Go API currently uses Markdown/source documentation rather than a generated Swagger UI.

## 18. View logs

All services:

```powershell
docker compose logs -f --tail=200
```

Specific service:

```powershell
docker compose logs -f --tail=200 api
docker compose logs -f --tail=200 orchestrator
docker compose logs -f --tail=200 ai-engine
docker compose logs -f --tail=200 recon-worker
docker compose logs -f --tail=200 postgres
docker compose logs -f --tail=200 nats
```

## 19. Stop the stack

Preserve volumes:

```powershell
docker compose down
```

Equivalent:

```powershell
make down
```

## 20. Full local reset

Warning: this deletes the local PostgreSQL, NATS and artifact volumes.

```powershell
docker compose down -v --remove-orphans
```

Then rebuild:

```powershell
docker compose up -d --build
```

## 21. Run component tests

Go + Python:

```powershell
make test
```

Go only:

```powershell
make go-test
```

Python only:

```powershell
make py-test
```

C++:

```powershell
make cpp-build
```

The GitHub Actions pipeline additionally runs database migration validation, S3-compatible storage tests, quality/accuracy gates, C++ smoke tests and the full Docker E2E workflow.

## 22. Classification benchmarks

Rules or ModernBERT accuracy:

```powershell
python benchmarks/classification_evaluate.py benchmarks/fixtures/classification_smoke.jsonl --backend rules
```

ModernBERT latency after downloading the model:

```powershell
python benchmarks/classification_latency.py --backend modernbert --provider cpu
```

For an NVIDIA/CUDA environment, use the CUDA-enabled ONNX Runtime setup and:

```powershell
python benchmarks/classification_latency.py --backend modernbert --provider cuda
```

Do not claim production latency from CI. Benchmark the real deployment hardware and representative documents.

## 23. Common troubleshooting

### API returns 503 from /health/ready

Inspect the returned check that is false, then run:

```powershell
docker compose ps
docker compose logs --tail=200 api
docker compose logs --tail=200 ai-engine
docker compose logs --tail=200 nats
docker compose logs --tail=200 postgres
```

### AI engine is unhealthy

Check:

```powershell
curl.exe http://localhost:8000/health/ready
docker compose logs --tail=300 ai-engine
```

If `DOCUMENT_CLASSIFIER_BACKEND=modernbert`, confirm:

```text
models/modernbert/config.json
models/modernbert/tokenizer.json
models/modernbert/onnx/model_int8.onnx
```

Otherwise return to:

```text
DOCUMENT_CLASSIFIER_BACKEND=rules
```

### Port already in use

Check whether these host ports are occupied:

```text
5432
4222
8222
8000
8080
```

Change the host-side Compose port mapping if another local service already owns one of them.

### Database migrations fail

```powershell
docker compose logs migrate
```

For a disposable local environment:

```powershell
docker compose down -v
docker compose up -d --build
```

### API/orchestrator repeatedly restarts

Check NATS and PostgreSQL first:

```powershell
docker compose ps
docker compose logs nats
docker compose logs postgres
docker compose logs api
docker compose logs orchestrator
```

The API and orchestrator connect to infrastructure during startup. Compose restart policies allow them to recover from short startup-order races.

## 24. Local security notes

The default Compose configuration is intentionally development-oriented:

```text
AUTH_MODE=disabled
POSTGRES_PASSWORD=rpd
local filesystem storage
no external TLS
```

Do not expose this default stack directly to the public internet.

Before production:

- enable authentication
- set a strong API-key pepper
- use managed secrets
- use TLS/reverse proxy
- configure production PostgreSQL credentials
- use production storage/S3 credentials
- perform tenant-isolation/security testing
- validate backup/restore
- establish retention policies

## 25. What is already proven by CI

The current GitHub Actions workflow performs:

1. Compose configuration validation.
2. Full Docker image build.
3. Full stack startup.
4. API readiness polling.
5. Invoice-to-ERP E2E processing.
6. Database migration/schema verification.
7. Python tests.
8. Go tests.
9. C++ tests/smoke.
10. S3-compatible storage integration.
11. Quality metrics and classification accuracy smoke gates.

A green E2E run is evidence that the services, internal DNS names, mounted native engine, shared storage, ports and business pipeline work together in a fresh Linux Docker environment.

## 26. Related documentation

- Architecture and implementation status: `docs/architecture/document-intelligence-core-guide.md`
- ERP/reconciliation API: `docs/api/erp-reconciliation-mvp.md`
- Local ModernBERT model notes: `models/README.md`
- Project quick start: `README.md`

## 27. Recommended local verification checklist

After every significant infrastructure change:

```text
[ ] docker compose config
[ ] docker compose up -d --build
[ ] docker compose ps
[ ] GET :8080/health/ready returns ready=true
[ ] GET :8000/health/ready returns ready=true
[ ] run tests/end-to-end/e2e_smoke.py
[ ] inspect job steps including CLASSIFY
[ ] verify canonical result
[ ] verify ERP reconciliation/export
[ ] docker compose down
```

If all checks pass, the current local MVP runtime is healthy.
