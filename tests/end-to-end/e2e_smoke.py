from __future__ import annotations

import csv
import json
import os
import tempfile
import time
from pathlib import Path

import requests
from reportlab.pdfgen import canvas


BASE_URL = os.environ.get("RPD_BASE_URL", "http://127.0.0.1:8080").rstrip("/")
TIMEOUT_SECONDS = int(os.environ.get("RPD_E2E_TIMEOUT", "240"))


def api(method: str, path: str, **kwargs):
    response = requests.request(method, BASE_URL + path, timeout=30, **kwargs)
    if response.status_code >= 400:
        raise RuntimeError(
            f"{method} {path} -> {response.status_code}: {response.text[:2000]}"
        )
    return response


def wait_for_job(job_id: str) -> dict:
    deadline = time.time() + TIMEOUT_SECONDS
    last = {}
    while time.time() < deadline:
        last = api("GET", f"/api/v1/jobs/{job_id}").json()
        status = last.get("status")
        if status in {"READY_FOR_RECONCILIATION", "REVIEW_REQUIRED", "FAILED", "CANCELLED"}:
            if status != "READY_FOR_RECONCILIATION":
                raise RuntimeError(f"document job ended in {status}: {last}")
            return last
        time.sleep(1)
    raise TimeoutError(f"document job {job_id} did not finish: {last}")


def wait_for_reconciliation(run_id: str) -> dict:
    deadline = time.time() + TIMEOUT_SECONDS
    last = {}
    while time.time() < deadline:
        last = api("GET", f"/api/v1/reconciliation-runs/{run_id}").json()
        status = ((last.get("run") or {}).get("status") or "").upper()
        if status in {"COMPLETED", "COMPLETED_WITH_EXCEPTIONS", "FAILED", "CANCELLED"}:
            if status not in {"COMPLETED", "COMPLETED_WITH_EXCEPTIONS"}:
                raise RuntimeError(f"reconciliation ended in {status}: {last}")
            return last
        time.sleep(1)
    raise TimeoutError(f"reconciliation {run_id} did not finish: {last}")


def build_invoice(path: Path) -> None:
    pdf = canvas.Canvas(str(path))
    pdf.setFont("Helvetica-Bold", 16)
    pdf.drawString(72, 760, "ABC Trading")
    pdf.setFont("Helvetica", 11)
    pdf.drawString(72, 730, "Invoice Number: INV-001")
    pdf.drawString(72, 710, "Invoice Date: 2026-11-16")
    pdf.drawString(72, 690, "Currency: PHP")
    pdf.drawString(72, 670, "Subtotal: 1000.00")
    pdf.drawString(72, 650, "Tax: 120.00")
    pdf.drawString(72, 630, "Grand Total: 1120.00")

    y = 580
    pdf.line(72, y + 20, 520, y + 20)
    pdf.drawString(78, y, "Description")
    pdf.drawString(300, y, "Quantity")
    pdf.drawString(370, y, "Unit Price")
    pdf.drawString(455, y, "Line Total")
    pdf.line(72, y - 8, 520, y - 8)
    pdf.drawString(78, y - 30, "Office Supplies")
    pdf.drawString(310, y - 30, "2")
    pdf.drawString(380, y - 30, "500.00")
    pdf.drawString(465, y - 30, "1000.00")
    pdf.line(72, y - 40, 520, y - 40)
    pdf.save()


def main() -> None:
    with tempfile.TemporaryDirectory(prefix="rpd-e2e-") as tmp:
        tmp_path = Path(tmp)
        invoice_path = tmp_path / "invoice.pdf"
        build_invoice(invoice_path)

        with invoice_path.open("rb") as handle:
            upload = api(
                "POST",
                "/api/v1/documents",
                files={"file": ("invoice.pdf", handle, "application/pdf")},
            ).json()

        document_id = upload["document_id"]
        job_id = upload["job_id"]
        wait_for_job(job_id)

        document = api("GET", f"/api/v1/documents/{document_id}").json()
        if (document.get("document_type") or "").strip() != "invoice":
            raise AssertionError(f"document was not classified as invoice: {document}")

        result = api("GET", f"/api/v1/documents/{document_id}/result").json()
        invoice_number = ((result.get("invoice_number") or {}).get("value") or "").strip()
        grand_total = ((result.get("grand_total") or {}).get("value") or "").strip()
        supplier = ((result.get("supplier_name") or {}).get("value") or "").strip()
        if invoice_number != "INV-001":
            raise AssertionError(f"unexpected invoice number: {invoice_number!r}")
        if grand_total != "1120.00":
            raise AssertionError(f"unexpected grand total: {grand_total!r}")
        if supplier != "ABC Trading":
            raise AssertionError(f"unexpected supplier: {supplier!r}")

        # Same bytes should be flagged as a possible duplicate, but still accepted.
        with invoice_path.open("rb") as handle:
            duplicate = api(
                "POST",
                "/api/v1/documents",
                files={"file": ("invoice.pdf", handle, "application/pdf")},
            ).json()
        if not duplicate.get("possible_duplicate"):
            raise AssertionError(f"duplicate upload was not flagged: {duplicate}")
        if document_id not in (duplicate.get("duplicate_document_ids") or []):
            raise AssertionError(f"original document missing from duplicate list: {duplicate}")

        # Reprocessing must create a new run/job while preserving the same document.
        reprocess = api(
            "POST",
            f"/api/v1/documents/{document_id}/reprocess",
            json={},
        ).json()
        if reprocess.get("document_id") != document_id:
            raise AssertionError(f"reprocess changed document id: {reprocess}")
        wait_for_job(reprocess["job_id"])

        dataset = api(
            "POST",
            "/api/v1/datasets",
            json={"name": "E2E ERP Dataset", "source_type": "erp"},
        ).json()
        dataset_id = dataset["id"]

        csv_path = tmp_path / "erp.csv"
        with csv_path.open("w", newline="", encoding="utf-8") as handle:
            writer = csv.writer(handle)
            writer.writerow(["supplier_name", "reference", "record_date", "currency", "amount"])
            writer.writerow(["ABC Trading", "INV-001", "2026-11-16", "PHP", "1120.00"])

        with csv_path.open("rb") as handle:
            imported = api(
                "POST",
                f"/api/v1/datasets/{dataset_id}/import",
                files={"file": ("erp.csv", handle, "text/csv")},
            ).json()
        if imported.get("inserted", 0) < 1:
            raise AssertionError(f"ERP row was not imported: {imported}")

        reconciliation = api(
            "POST",
            "/api/v1/reconciliation-runs",
            json={
                "name": "E2E Invoice Reconciliation",
                "target_dataset_id": dataset_id,
                "document_ids": [document_id],
                "absolute_tolerance": "0.01",
                "date_window_days": 3,
            },
        ).json()
        run_id = reconciliation["run_id"]
        wait_for_reconciliation(run_id)

        matches = api(
            "GET",
            f"/api/v1/reconciliation-runs/{run_id}/matches",
        ).json().get("items") or []
        if not matches:
            exceptions = api(
                "GET",
                f"/api/v1/reconciliation-runs/{run_id}/exceptions",
            ).json()
            raise AssertionError(f"expected reconciliation match; exceptions={exceptions}")

        export = api(
            "POST",
            f"/api/v1/reconciliation-runs/{run_id}/export?format=json",
        ).json()
        download_url = export["download_url"]
        downloaded = api("GET", download_url)
        payload = downloaded.json()
        if not payload:
            raise AssertionError("export payload was empty")

        steps = api("GET", f"/api/v1/jobs/{reprocess['job_id']}/steps").json().get("items") or []
        if not steps or steps[-1].get("status") != "COMPLETED":
            raise AssertionError(f"job steps did not complete: {steps}")
        classify_steps = [step for step in steps if step.get("step_name") == "CLASSIFY"]
        if not classify_steps or classify_steps[-1].get("status") != "COMPLETED":
            raise AssertionError(f"classification step did not complete: {steps}")

        artifacts = api("GET", f"/api/v1/documents/{document_id}/artifacts").json().get("items") or []
        if not any(item.get("artifact_type") == "CLASSIFICATION_JSON" for item in artifacts):
            raise AssertionError(f"classification artifact missing: {artifacts}")

        print(
            json.dumps(
                {
                    "document_id": document_id,
                    "job_id": job_id,
                    "reprocess_job_id": reprocess["job_id"],
                    "dataset_id": dataset_id,
                    "reconciliation_run_id": run_id,
                    "match_count": len(matches),
                    "status": "PASS",
                },
                indent=2,
            )
        )


if __name__ == "__main__":
    main()
