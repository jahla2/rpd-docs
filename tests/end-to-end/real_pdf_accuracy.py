from __future__ import annotations

import hashlib
import json
import os
import re
import subprocess
import sys
import tempfile
import time
from collections import Counter
from pathlib import Path

import requests
from PIL import Image
from reportlab.lib.utils import ImageReader
from reportlab.pdfgen import canvas

BASE_URL = os.getenv("RPD_BASE_URL", "http://127.0.0.1:8080").rstrip("/")
TIMEOUT = int(os.getenv("RPD_E2E_TIMEOUT", "300"))

FIXTURES = {
    "(16) Bluewater Property Vare Ltd - Koruline Screening Services.pdf": {
        "sha256": "21faa5ec6c239755a1001540069ebd4a8e46afecc4038c57c042456e556eee02",
        "issuer": "KoruLine Screening Ltd",
        "customer": "Bluewater Property Care Ltd",
        "statement_date_text": "31 July 2026",
        "statement_date": "2026-07-31",
        "gst": "110-240-510",
        "currency": "NZD",
        "closing_balance": "304.35",
        "rows": [
            {"date_text": "04 May 2026", "date": "2026-05-04", "invoice_number": "KOR-26000", "reference": "KOR-AK5800", "due_text": "03 Jun 2026", "invoice_amount": "118.40", "payment": "0.00", "balance": "118.40"},
            {"date_text": "07 Jul 2026", "date": "2026-07-07", "invoice_number": "KOR-26007", "reference": "KOR-WL5817", "due_text": "06 Aug 2026", "invoice_amount": "94.75", "payment": "20.00", "balance": "74.75"},
            {"date_text": "23 Jul 2026", "date": "2026-07-23", "invoice_number": "KOR-26014", "reference": "KOR-CH5834", "due_text": "22 Aug 2026", "invoice_amount": "151.20", "payment": "40.00", "balance": "111.20"},
        ],
    },
    "(17) Greenstone Facilities - BluePeak.pdf": {
        "sha256": "e7d0439ea52101ab5a311fd0f0107e76cc1063c38cd2523f1fe86ed8a82e32d5",
        "issuer": "BluePeak Verification Ltd",
        "customer": "Greenstone Facilities Ltd",
        "statement_date_text": "28 July 2026",
        "statement_date": "2026-07-28",
        "gst": "111-247-523",
        "currency": "NZD",
        "closing_balance": "358.25",
        "rows": [
            {"date_text": "01 May 2026", "date": "2026-05-01", "invoice_number": "BLU-26031", "reference": "BLU-AK5811", "due_text": "31 May 2026", "invoice_amount": "125.75", "payment": "0.00", "balance": "125.75"},
            {"date_text": "04 Jul 2026", "date": "2026-07-04", "invoice_number": "BLU-26038", "reference": "BLU-WL5828", "due_text": "03 Aug 2026", "invoice_amount": "99.90", "payment": "0.00", "balance": "99.90"},
            {"date_text": "20 Jul 2026", "date": "2026-07-20", "invoice_number": "BLU-26045", "reference": "BLU-CH5845", "due_text": "19 Aug 2026", "invoice_amount": "157.60", "payment": "25.00", "balance": "132.60"},
        ],
    },
    "(18) Rimu Property - HarbourShield.pdf": {
        "sha256": "6a727c6c692a67d4f3626c4e05db2153eadcf400fefdd25c2c7a8f972b3391a2",
        "issuer": "HarbourShield Checks Ltd",
        "customer": "Rimu Property Partners Ltd",
        "statement_date_text": "25 July 2026",
        "statement_date": "2026-07-25",
        "gst": "112-254-536",
        "currency": "NZD",
        "closing_balance": "362.15",
        "rows": [
            {"date_text": "28 Apr 2026", "date": "2026-04-28", "invoice_number": "HAR-26062", "reference": "HAR-AK5822", "due_text": "28 May 2026", "invoice_amount": "133.10", "payment": "0.00", "balance": "133.10"},
            {"date_text": "01 Jul 2026", "date": "2026-07-01", "invoice_number": "HAR-26069", "reference": "HAR-WL5839", "due_text": "31 Jul 2026", "invoice_amount": "105.05", "payment": "0.00", "balance": "105.05"},
            {"date_text": "17 Jul 2026", "date": "2026-07-17", "invoice_number": "HAR-26076", "reference": "HAR-CH5856", "due_text": "16 Aug 2026", "invoice_amount": "164.00", "payment": "40.00", "balance": "124.00"},
        ],
    },
    "(19) Southfield Building Care - Southern Arc.pdf": {
        "sha256": "001a1d1b52d4ac9b4389394e93e2c8fd51c6a8e84470963a7ddb7e60afb783d5",
        "issuer": "Southern Arc Screening Ltd",
        "customer": "Southfield Building Care Ltd",
        "statement_date_text": "22 July 2026",
        "statement_date": "2026-07-22",
        "gst": "113-261-549",
        "currency": "NZD",
        "closing_balance": "376.05",
        "rows": [
            {"date_text": "25 Apr 2026", "date": "2026-04-25", "invoice_number": "SOU-26093", "reference": "SOU-AK5833", "due_text": "25 May 2026", "invoice_amount": "140.45", "payment": "0.00", "balance": "140.45"},
            {"date_text": "28 Jun 2026", "date": "2026-06-28", "invoice_number": "SOU-26100", "reference": "SOU-WL5850", "due_text": "28 Jul 2026", "invoice_amount": "110.20", "payment": "20.00", "balance": "90.20"},
            {"date_text": "14 Jul 2026", "date": "2026-07-14", "invoice_number": "SOU-26107", "reference": "SOU-CH5867", "due_text": "13 Aug 2026", "invoice_amount": "170.40", "payment": "25.00", "balance": "145.40"},
        ],
    },
}


def api(method: str, path: str, **kwargs):
    response = requests.request(method, BASE_URL + path, timeout=120, **kwargs)
    response.raise_for_status()
    return response


def sha256(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def rasterize_pdf(source: Path, destination: Path, dpi: int = 200) -> None:
    with tempfile.TemporaryDirectory(prefix="rpd-raster-") as tmp:
        prefix = Path(tmp) / "page"
        subprocess.run(
            ["pdftoppm", "-f", "1", "-singlefile", "-r", str(dpi), "-png", str(source), str(prefix)],
            check=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
        )
        image_path = prefix.with_suffix(".png")
        with Image.open(image_path) as image:
            width, height = image.size
        page_size = (width * 72.0 / dpi, height * 72.0 / dpi)
        pdf = canvas.Canvas(str(destination), pagesize=page_size)
        pdf.drawImage(ImageReader(str(image_path)), 0, 0, width=page_size[0], height=page_size[1])
        pdf.showPage()
        pdf.save()


def wait_for_job(job_id: str) -> dict:
    deadline = time.time() + TIMEOUT
    last = {}
    terminal = {"READY_FOR_RECONCILIATION", "COMPLETED", "REVIEW_REQUIRED", "FAILED", "CANCELLED"}
    while time.time() < deadline:
        last = api("GET", f"/api/v1/jobs/{job_id}").json()
        if last.get("status") in terminal:
            return last
        time.sleep(1)
    raise TimeoutError(f"job {job_id} timed out: {last}")


def norm(value: object) -> str:
    value = str(value or "").lower()
    value = re.sub(r"[^a-z0-9.]+", " ", value)
    return " ".join(value.split())


def field_value(fields: dict, key: str) -> str:
    value = fields.get(key) or {}
    return str(value.get("value") or "").strip()


def critical_tokens(gt: dict) -> list[str]:
    tokens = [
        gt["issuer"], gt["customer"], gt["statement_date_text"], gt["gst"],
        gt["currency"], gt["closing_balance"],
    ]
    for row in gt["rows"]:
        tokens.extend([
            row["date_text"], row["invoice_number"], row["reference"], row["due_text"],
            row["invoice_amount"], row["payment"], row["balance"],
        ])
    return tokens


def multiset_recall(expected: list[str], actual_text: str) -> tuple[int, int, list[str]]:
    normalized = norm(actual_text)
    counts = Counter(norm(item) for item in expected)
    matched = 0
    misses: list[str] = []
    for token, required in counts.items():
        found = normalized.count(token)
        hit = min(found, required)
        matched += hit
        misses.extend([token] * (required - hit))
    return matched, sum(counts.values()), misses


def canonical_score(result: dict, gt: dict) -> tuple[int, int, dict]:
    fields = result.get("fields") or {}
    expected = {
        "supplier_name": gt["issuer"],
        "statement_date": gt["statement_date"],
        "currency": gt["currency"],
        "closing_balance": gt["closing_balance"],
    }
    actual = {key: field_value(fields, key) for key in expected}
    matches = {
        key: norm(actual[key]) == norm(value)
        for key, value in expected.items()
    }
    return sum(matches.values()), len(matches), {"expected": expected, "actual": actual, "matches": matches}


def transaction_score(result: dict, gt: dict) -> tuple[int, int, dict]:
    rows = ((result.get("tables") or {}).get("transactions") or [])
    expected_rows = gt["rows"]
    # Semantically correct source-to-canonical targets. These are deliberately
    # not adjusted to reward a wrong alias such as Reference -> invoice_number.
    keys = {
        "date": "date",
        "invoice_number": "invoice_number",
        "payment": "credit",
        "balance": "balance",
    }
    matched = 0
    total = len(expected_rows) * len(keys)
    detail = []
    for index, expected in enumerate(expected_rows):
        actual = rows[index] if index < len(rows) else {}
        checks = {}
        for source_key, target_key in keys.items():
            expected_value = expected[source_key]
            actual_value = str(actual.get(target_key) or "").strip()
            ok = norm(actual_value) == norm(expected_value)
            matched += int(ok)
            checks[target_key] = {
                "expected": expected_value,
                "actual": actual_value,
                "match": ok,
            }
        detail.append(checks)
    return matched, total, {"row_count": len(rows), "rows": detail, "raw_rows": rows}


def read_ocr_text(document_id: str) -> tuple[str, list[dict]]:
    artifacts = api("GET", f"/api/v1/documents/{document_id}/artifacts").json().get("items") or []
    ocr_artifacts = [item for item in artifacts if item.get("artifact_type") == "OCR_JSON"]
    texts = []
    runs = []
    for item in ocr_artifacts:
        artifact_id = item.get("id")
        if not artifact_id:
            continue
        payload = api("GET", f"/api/v1/artifacts/{artifact_id}/content").json()
        blocks = payload.get("blocks") or []
        texts.extend(str(block.get("text") or "") for block in blocks)
        runs.append({
            "artifact_id": artifact_id,
            "engine": payload.get("engine"),
            "model": payload.get("model"),
            "block_count": len(blocks),
        })
    return "\n".join(texts), runs


def run_one(source: Path, gt: dict, workdir: Path) -> dict:
    actual_hash = sha256(source)
    if actual_hash != gt["sha256"]:
        raise RuntimeError(f"fixture SHA mismatch for {source.name}: {actual_hash}")

    image_pdf = workdir / ("ocr-" + source.name)
    rasterize_pdf(source, image_pdf)

    with image_pdf.open("rb") as handle:
        upload = api(
            "POST",
            "/api/v1/documents",
            files={"file": (image_pdf.name, handle, "application/pdf")},
            data={"document_type": "supplier_statement"},
        ).json()

    document_id = upload["document_id"]
    job_id = upload["job_id"]
    job = wait_for_job(job_id)
    if job.get("status") == "FAILED":
        raise RuntimeError(f"pipeline failed for {source.name}: {job}")

    pages = api("GET", f"/api/v1/jobs/{job_id}/pages").json().get("items") or []
    routes = [str(page.get("route") or "") for page in pages]
    if not routes or any(route != "ocr" for route in routes):
        raise RuntimeError(f"OCR route was not forced for {source.name}: {routes}")

    document = api("GET", f"/api/v1/documents/{document_id}").json()
    result = api("GET", f"/api/v1/documents/{document_id}/result").json()
    ocr_text, ocr_runs = read_ocr_text(document_id)

    ocr_hit, ocr_total, ocr_misses = multiset_recall(critical_tokens(gt), ocr_text)
    canonical_hit, canonical_total, canonical = canonical_score(result, gt)
    tx_hit, tx_total, transactions = transaction_score(result, gt)

    return {
        "file": source.name,
        "fixture_sha256": actual_hash,
        "route": routes,
        "ocr_runs": ocr_runs,
        "job_status": job.get("status"),
        "document_type": document.get("document_type"),
        "classification_correct": document.get("document_type") == "supplier_statement",
        "ocr_critical_token_recall": round(ocr_hit / ocr_total, 4) if ocr_total else 0,
        "ocr_tokens_matched": ocr_hit,
        "ocr_tokens_total": ocr_total,
        "ocr_misses": ocr_misses,
        "canonical_field_accuracy": round(canonical_hit / canonical_total, 4),
        "canonical_fields_matched": canonical_hit,
        "canonical_fields_total": canonical_total,
        "canonical": canonical,
        "transaction_semantic_accuracy": round(tx_hit / tx_total, 4),
        "transaction_fields_matched": tx_hit,
        "transaction_fields_total": tx_total,
        "transactions": transactions,
        "requires_review": bool(result.get("requires_review")),
    }


def main() -> None:
    if len(sys.argv) != 2:
        raise SystemExit("usage: real_pdf_accuracy.py FIXTURE_DIR")
    fixture_dir = Path(sys.argv[1])
    results = []
    with tempfile.TemporaryDirectory(prefix="rpd-real-accuracy-") as tmp:
        workdir = Path(tmp)
        for filename, gt in FIXTURES.items():
            source = fixture_dir / filename
            if not source.exists():
                raise FileNotFoundError(source)
            item = run_one(source, gt, workdir)
            results.append(item)
            print(json.dumps({
                "file": filename,
                "status": item["job_status"],
                "type": item["document_type"],
                "route": item["route"],
                "ocr": item["ocr_critical_token_recall"],
                "canonical": item["canonical_field_accuracy"],
                "transactions": item["transaction_semantic_accuracy"],
            }, sort_keys=True))

    def aggregate(matched_key: str, total_key: str) -> dict:
        matched = sum(int(item[matched_key]) for item in results)
        total = sum(int(item[total_key]) for item in results)
        return {"matched": matched, "total": total, "accuracy": round(matched / total, 4) if total else 0}

    report = {
        "schema_version": "1.0",
        "test_mode": "exact uploaded PDF bytes -> SHA verify -> 200 DPI raster -> image-only PDF -> full pipeline",
        "ocr_route_asserted": True,
        "classification": {
            "correct": sum(int(item["classification_correct"]) for item in results),
            "total": len(results),
        },
        "ocr_critical_token_recall": aggregate("ocr_tokens_matched", "ocr_tokens_total"),
        "canonical_field_accuracy": aggregate("canonical_fields_matched", "canonical_fields_total"),
        "transaction_semantic_accuracy": aggregate("transaction_fields_matched", "transaction_fields_total"),
        "results": results,
    }

    out = Path("/tmp/real-pdf-accuracy.json")
    out.write_text(json.dumps(report, indent=2), encoding="utf-8")
    print("REAL_PDF_ACCURACY_REPORT")
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
