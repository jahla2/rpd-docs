# ERP Import and Reconciliation MVP API

This flow assumes document processing has already produced canonical invoice JSON.

## 1. Create an ERP dataset

```http
POST /api/v1/datasets
Content-Type: application/json

{
  "name": "AP Export November 2026",
  "source_type": "erp"
}
```

Response includes the dataset `id`.

## 2. Import CSV or XLSX

```http
POST /api/v1/datasets/{datasetId}/import
Content-Type: multipart/form-data
```

Multipart fields:

- `file`: required CSV or XLSX
- `mapping`: optional JSON object mapping source headers to canonical ERP fields

Example override:

```json
{
  "Vendor Text": "supplier_name",
  "RefNum": "reference",
  "Posting Dt": "record_date",
  "CCY": "currency",
  "Gross": "amount"
}
```

Supported canonical ERP fields:

```text
external_record_id
record_date
supplier_name
reference
currency
amount
```

The importer automatically recognizes common aliases such as `Vendor`, `Invoice No`, `Posting Date`, `Currency`, and `Gross Amount`.

Rows are normalized before insertion:

- supplier and reference keys are uppercase alphanumeric forms
- amounts are parsed with decimal-safe rational arithmetic
- dates are normalized to PostgreSQL dates
- duplicate rows in the same dataset are skipped using a deterministic SHA-256 row hash
- malformed rows are returned in the import response instead of silently inserted

## 3. Inspect imported records

```http
GET /api/v1/datasets/{datasetId}
GET /api/v1/datasets/{datasetId}/records?limit=100&offset=0
```

## 4. Start invoice-to-ERP reconciliation

```http
POST /api/v1/reconciliation-runs
Content-Type: application/json

{
  "name": "November Invoice Reconciliation",
  "target_dataset_id": "{datasetId}",
  "document_ids": [
    "{canonicalDocumentId1}",
    "{canonicalDocumentId2}"
  ],
  "absolute_tolerance": "0.01",
  "date_window_days": 3,
  "auto_accept_composite": false
}
```

The synchronous MVP accepts up to 500 documents per request.

Candidate generation is performed in PostgreSQL before deeper comparison. The query uses:

- normalized invoice/reference
- normalized supplier
- currency
- amount tolerance
- date window

This avoids a full document × ERP cross join.

## 5. Match statuses

Possible persisted match statuses:

```text
MATCHED_EXACT
MATCHED_TOLERANCE
MATCHED_COMPOSITE
SUGGESTED_MATCH
APPROVED
REJECTED
```

Possible exception types include:

```text
UNMATCHED
DUPLICATE_OR_AMBIGUOUS
REVIEW_REQUIRED
INVALID_SOURCE
PROCESSING_ERROR
```

Equal top scores are treated as ambiguous and routed to review instead of being automatically approved.

## 6. Read reconciliation results

```http
GET /api/v1/reconciliation-runs/{runId}
GET /api/v1/reconciliation-runs/{runId}/matches
GET /api/v1/reconciliation-runs/{runId}/exceptions
```

## 7. Approve or reject a suggested match

```http
POST /api/v1/reconciliation-runs/{runId}/matches/{matchId}/approve
X-Actor-ID: reviewer@example
```

or:

```http
POST /api/v1/reconciliation-runs/{runId}/matches/{matchId}/reject
X-Actor-ID: reviewer@example
```

## 8. Review queue

```http
GET /api/v1/review-tasks?status=OPEN&limit=100
```

Resolve:

```http
POST /api/v1/review-tasks/{taskId}/resolve
X-Actor-ID: reviewer@example
Content-Type: application/json

{
  "resolution": {
    "decision": "confirmed_unmatched",
    "notes": "ERP transaction has not been posted yet"
  }
}
```

Resolution is written to the review task and an audit event is created.

## 9. Export reconciliation report

Create an export:

```http
POST /api/v1/reconciliation-runs/{runId}/export?format=xlsx
```

Supported values:

```text
json
csv
xlsx
```

The response returns an export ID and download URL.

Read metadata:

```http
GET /api/v1/exports/{exportId}
```

Download:

```http
GET /api/v1/exports/{exportId}/download
```

XLSX exports contain separate `Matches` and `Exceptions` worksheets.

## MVP sequence

```text
Processed invoice
→ canonical JSON
→ ERP CSV/XLSX dataset
→ automatic ERP header mapping
→ canonical ERP rows
→ indexed candidate generation
→ exact/tolerance/composite scoring
→ ambiguity/duplicate detection
→ matches + exceptions
→ review queue
→ JSON/CSV/XLSX export
```
