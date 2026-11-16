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


def wait_for_document_status(job_id: str, expected_status: str) -> dict:
    deadline = time.time() + TIMEOUT_SECONDS
    last = {}
    terminal = {"READY_FOR_RECONCILIATION", "COMPLETED", "REVIEW_REQUIRED", "FAILED", "CANCELLED"}
    while time.time() < deadline:
        last = api("GET", f"/api/v1/jobs/{job_id}").json()
        status = last.get("status")
        if status in terminal:
            if status != expected_status:
                raise RuntimeError(f"document job ended in {status}, expected {expected_status}: {last}")
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


def build_supplier_statement(path: Path) -> None:
    pdf = canvas.Canvas(str(path))
    pdf.setFont("Helvetica-Bold", 16)
    pdf.drawString(72, 760, "Statement of Account")
    pdf.setFont("Helvetica", 11)
    pdf.drawString(72, 730, "Supplier: ABC Trading")
    pdf.drawString(72, 710, "Statement Date: 2026-11-30")
    pdf.drawString(72, 690, "Currency: PHP")
    pdf.drawString(72, 670, "Previous Balance: 5000.00")
    pdf.drawString(72, 650, "Current Balance: 6500.00")

    y = 590
    pdf.line(72, y + 20, 520, y + 20)
    pdf.drawString(78, y, "Date")
    pdf.drawString(180, y, "Invoice No")
    pdf.drawString(290, y, "Debit")
    pdf.drawString(370, y, "Credit")
    pdf.drawString(450, y, "Balance")
    pdf.line(72, y - 8, 520, y - 8)
    pdf.drawString(78, y - 30, "2026-11-10")
    pdf.drawString(180, y - 30, "INV-009")
    pdf.drawString(290, y - 30, "1500.00")
    pdf.drawString(450, y - 30, "6500.00")
    pdf.line(72, y - 40, 520, y - 40)
    pdf.save()


def build_supplier_statement_layout_b(
    path: Path,
    *,
    statement_date: str,
    closing_balance: str,
    closing_label: str = "Current Balance",
    invoice_number: str = "INV-B01",
) -> None:
    # Landscape layout with label/value pairs split across lines.
    pdf = canvas.Canvas(str(path), pagesize=(792, 612))
    pdf.setFont("Helvetica-Bold", 16)
    pdf.drawString(50, 555, "Statement of Account")
    pdf.setFont("Helvetica", 11)

    pdf.drawString(50, 515, "Supplier")
    pdf.drawString(50, 492, "ABC Trading")
    pdf.drawString(500, 515, "Statement Date")
    pdf.drawString(500, 492, statement_date)
    pdf.drawString(50, 455, "Currency: PHP")
    pdf.drawString(250, 455, "Previous Balance: 5000.00")
    pdf.drawString(500, 455, closing_label)
    pdf.drawString(500, 432, closing_balance)

    y = 350
    pdf.line(45, y + 20, 745, y + 20)
    pdf.drawString(50, y, "Date")
    pdf.drawString(185, y, "Invoice No")
    pdf.drawString(360, y, "Debit")
    pdf.drawString(470, y, "Credit")
    pdf.drawString(610, y, "Balance")
    pdf.line(45, y - 8, 745, y - 8)
    pdf.drawString(50, y - 30, "2026-11-10")
    pdf.drawString(185, y - 30, invoice_number)
    pdf.drawString(360, y - 30, "1500.00")
    pdf.drawString(610, y - 30, closing_balance)
    pdf.line(45, y - 40, 745, y - 40)
    pdf.save()


def upload_pdf(path: Path, filename: str) -> dict:
    with path.open("rb") as handle:
        return api(
            "POST",
            "/api/v1/documents",
            files={"file": (filename, handle, "application/pdf")},
        ).json()


def main() -> None:
    with tempfile.TemporaryDirectory(prefix="rpd-e2e-") as tmp:
        tmp_path = Path(tmp)

        # Document-type configuration is tenant-scoped and persisted in PostgreSQL.
        document_type = api(
            "POST",
            "/api/v1/document-types",
            json={
                "code": "receipt",
                "name": "Receipt",
                "schema_version": 1,
                "active": True,
            },
        ).json()
        document_type_id = document_type["id"]

        listed_document_types = api("GET", "/api/v1/document-types").json().get("items") or []
        if document_type_id not in {item.get("id") for item in listed_document_types}:
            raise AssertionError(f"document type missing from list: {listed_document_types}")

        fetched_document_type = api(
            "GET",
            f"/api/v1/document-types/{document_type_id}",
        ).json()
        if fetched_document_type.get("code") != "receipt":
            raise AssertionError(f"unexpected document type: {fetched_document_type}")

        field_definition = api(
            "POST",
            "/api/v1/field-definitions",
            json={
                "document_type_id": document_type_id,
                "field_name": "receipt_number",
                "display_name": "Receipt Number",
                "data_type": "identifier",
                "required": True,
                "repeating": False,
            },
        ).json()
        field_definition_id = field_definition["id"]

        field_definitions = api(
            "GET",
            f"/api/v1/field-definitions?document_type_id={document_type_id}",
        ).json().get("items") or []
        if field_definition_id not in {item.get("id") for item in field_definitions}:
            raise AssertionError(f"field definition missing from list: {field_definitions}")

        updated_field_definition = api(
            "PUT",
            f"/api/v1/field-definitions/{field_definition_id}",
            json={
                "document_type_id": document_type_id,
                "field_name": "receipt_number",
                "display_name": "Receipt Reference",
                "data_type": "identifier",
                "required": True,
                "repeating": False,
            },
        ).json()
        if updated_field_definition.get("display_name") != "Receipt Reference":
            raise AssertionError(f"field definition update failed: {updated_field_definition}")

        delete_field_definition = api(
            "DELETE",
            f"/api/v1/field-definitions/{field_definition_id}",
        )
        if delete_field_definition.status_code != 204:
            raise AssertionError(
                f"field definition delete failed: {delete_field_definition.status_code}"
            )

        updated_document_type = api(
            "PUT",
            f"/api/v1/document-types/{document_type_id}",
            json={
                "code": "receipt",
                "name": "Receipt Document",
                "schema_version": 1,
                "active": False,
            },
        ).json()
        if updated_document_type.get("name") != "Receipt Document" or updated_document_type.get("active") is not False:
            raise AssertionError(f"document type update failed: {updated_document_type}")

        immutable_identity_response = requests.put(
            BASE_URL + f"/api/v1/document-types/{document_type_id}",
            timeout=30,
            json={
                "code": "invoice",
                "name": "Receipt Document",
                "schema_version": 1,
                "active": False,
            },
        )
        if immutable_identity_response.status_code != 409:
            raise AssertionError(
                "document type identity mutation was not rejected: "
                f"{immutable_identity_response.status_code} {immutable_identity_response.text}"
            )

        mapping_template = api(
            "POST",
            "/api/v1/mapping-templates",
            json={
                "name": "e2e-invoice-preview",
                "document_type": "invoice",
                "config": {
                    "columns": [
                        {
                            "source_aliases": ["Inv Ref"],
                            "target": "invoice_number",
                            "type": "identifier",
                            "required": True,
                            "operations": [
                                {"operation": "normalize", "config": {"case": "upper"}}
                            ],
                        },
                        {
                            "source_aliases": ["Invoice Date"],
                            "target": "invoice_date",
                            "type": "date",
                            "required": True,
                            "operations": [{"operation": "date"}],
                        },
                        {
                            "source_aliases": ["Total"],
                            "target": "line_total",
                            "type": "decimal",
                            "operations": [{"operation": "decimal"}],
                        },
                    ]
                },
            },
        ).json()
        mapping_template_id = mapping_template["id"]

        mapping_preview = api(
            "POST",
            f"/api/v1/mapping-templates/{mapping_template_id}/preview",
            json={
                "headers": ["Inv Ref", "Invoice Date", "Total"],
                "samples": {
                    "Inv Ref": [" inv-001 "],
                    "Invoice Date": ["16/11/2026"],
                    "Total": ["₱ 1,250.50"],
                },
            },
        ).json()
        if mapping_preview.get("template_status") != "DRAFT":
            raise AssertionError(f"draft mapping template could not be previewed: {mapping_preview}")
        if mapping_preview.get("validation_errors"):
            raise AssertionError(f"mapping preview validation failed: {mapping_preview}")
        if mapping_preview.get("confidence") != 1:
            raise AssertionError(f"mapping preview confidence was not exact: {mapping_preview}")
        preview_rows = mapping_preview.get("sample_transformed_rows") or []
        if not preview_rows or preview_rows[0].get("invoice_number") != "INV-001":
            raise AssertionError(f"mapping preview normalize transform failed: {mapping_preview}")
        if preview_rows[0].get("invoice_date") != "2026-11-16":
            raise AssertionError(f"mapping preview date transform failed: {mapping_preview}")
        if preview_rows[0].get("line_total") != "1250.50":
            raise AssertionError(f"mapping preview decimal transform failed: {mapping_preview}")

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

        # A supplier statement must auto-classify and use the statement schema,
        # not the invoice extractor. No document_type hint is supplied.
        statement_path = tmp_path / "supplier-statement.pdf"
        build_supplier_statement(statement_path)
        with statement_path.open("rb") as handle:
            statement_upload = api(
                "POST",
                "/api/v1/documents",
                files={"file": ("supplier-statement.pdf", handle, "application/pdf")},
            ).json()

        statement_document_id = statement_upload["document_id"]
        statement_job_id = statement_upload["job_id"]
        wait_for_document_status(statement_job_id, "COMPLETED")

        statement_document = api(
            "GET",
            f"/api/v1/documents/{statement_document_id}",
        ).json()
        if statement_document.get("document_type") != "supplier_statement":
            raise AssertionError(f"statement was not auto-routed: {statement_document}")

        statement_result = api(
            "GET",
            f"/api/v1/documents/{statement_document_id}/result",
        ).json()
        statement_fields = statement_result.get("fields") or {}
        supplier_name = ((statement_fields.get("supplier_name") or {}).get("value") or "").strip()
        closing_balance = ((statement_fields.get("closing_balance") or {}).get("value") or "").strip()
        statement_date = ((statement_fields.get("statement_date") or {}).get("value") or "").strip()
        if supplier_name != "ABC Trading":
            raise AssertionError(f"unexpected statement supplier: {supplier_name!r}")
        if closing_balance != "6500.00":
            raise AssertionError(f"unexpected statement closing balance: {closing_balance!r}")
        if statement_date != "2026-11-30":
            raise AssertionError(f"unexpected statement date: {statement_date!r}")

        statement_steps = api(
            "GET",
            f"/api/v1/jobs/{statement_job_id}/steps",
        ).json().get("items") or []
        if not any(
            step.get("step_name") == "CLASSIFY" and step.get("status") == "COMPLETED"
            for step in statement_steps
        ):
            raise AssertionError(f"statement classification step missing: {statement_steps}")

        # Learn a second supplier layout. The first two validated observations
        # create/promote its profile; the third changes a field label and must
        # recover closing_balance using the learned normalized region.
        learned_layout_ids = []
        for index, (date_value, balance_value) in enumerate(
            [("2026-12-01", "7100.00"), ("2026-12-02", "7200.00")],
            start=1,
        ):
            layout_path = tmp_path / f"supplier-layout-b-{index}.pdf"
            build_supplier_statement_layout_b(
                layout_path,
                statement_date=date_value,
                closing_balance=balance_value,
                invoice_number=f"INV-B0{index}",
            )
            learned_upload = upload_pdf(layout_path, layout_path.name)
            wait_for_document_status(learned_upload["job_id"], "COMPLETED")
            learned_result = api(
                "GET",
                f"/api/v1/documents/{learned_upload['document_id']}/result",
            ).json()
            learned_fields = learned_result.get("fields") or {}
            if ((learned_fields.get("closing_balance") or {}).get("value") or "") != balance_value:
                raise AssertionError(f"layout B learning extraction failed: {learned_result}")
            learned_layout_ids.append(learned_upload["document_id"])

        adaptive_path = tmp_path / "supplier-layout-b-adaptive.pdf"
        build_supplier_statement_layout_b(
            adaptive_path,
            statement_date="2026-12-03",
            closing_balance="7300.00",
            closing_label="Amount Outstanding",
            invoice_number="INV-B03",
        )
        adaptive_upload = upload_pdf(adaptive_path, adaptive_path.name)
        wait_for_document_status(adaptive_upload["job_id"], "COMPLETED")
        adaptive_result = api(
            "GET",
            f"/api/v1/documents/{adaptive_upload['document_id']}/result",
        ).json()
        adaptive_layout = adaptive_result.get("layout") or {}
        adaptive_fields = adaptive_result.get("fields") or {}
        adaptive_closing = adaptive_fields.get("closing_balance") or {}

        if adaptive_layout.get("matched") is not True:
            raise AssertionError(f"learned layout was not matched: {adaptive_result}")
        if float(adaptive_layout.get("score") or 0.0) < 0.72:
            raise AssertionError(f"learned layout score too low: {adaptive_layout}")
        if (adaptive_closing.get("value") or "") != "7300.00":
            raise AssertionError(f"layout profile did not recover closing balance: {adaptive_result}")
        if ((adaptive_closing.get("evidence") or {}).get("method") or "") != "layout_profile_region":
            raise AssertionError(f"closing balance did not use learned region: {adaptive_closing}")

        # Safe split transform must execute in the real extraction pipeline,
        # not only in mapping preview.
        split_template = api(
            "POST",
            "/api/v1/mapping-templates",
            json={
                "name": "e2e-split-runtime",
                "document_type": "invoice",
                "config": {
                    "columns": [
                        {
                            "source_aliases": ["Invoice Number"],
                            "target": "po_number",
                            "type": "identifier",
                            "required": False,
                            "operations": [
                                {
                                    "operation": "split",
                                    "config": {
                                        "delimiter": "-",
                                        "index": 0,
                                        "trim": True,
                                    },
                                }
                            ],
                        },
                        {
                            "source_aliases": ["Invoice Number"],
                            "target": "invoice_number",
                            "type": "identifier",
                            "required": True,
                            "operations": [
                                {
                                    "operation": "split",
                                    "config": {
                                        "delimiter": "-",
                                        "index": 1,
                                        "trim": True,
                                    },
                                }
                            ],
                        }
                    ]
                },
            },
        ).json()
        split_template_id = split_template["id"]
        activated_split_template = api(
            "POST",
            f"/api/v1/mapping-templates/{split_template_id}/activate",
        ).json()
        if activated_split_template.get("status") != "ACTIVE":
            raise AssertionError(f"split template activation failed: {activated_split_template}")

        split_reprocess = api(
            "POST",
            f"/api/v1/documents/{document_id}/reprocess",
            json={"mapping_template_id": split_template_id},
        ).json()
        wait_for_job(split_reprocess["job_id"])
        split_result = api(
            "GET",
            f"/api/v1/documents/{document_id}/result",
        ).json()
        split_invoice_number = (
            (split_result.get("invoice_number") or {}).get("value") or ""
        ).strip()
        split_po_number = (
            (split_result.get("po_number") or {}).get("value") or ""
        ).strip()
        if split_invoice_number != "001" or split_po_number != "INV":
            raise AssertionError(
                f"multi-target split was not applied by runtime: {split_result}"
            )

        print(
            json.dumps(
                {
                    "document_id": document_id,
                    "job_id": job_id,
                    "reprocess_job_id": reprocess["job_id"],
                    "dataset_id": dataset_id,
                    "reconciliation_run_id": run_id,
                    "match_count": len(matches),
                    "statement_document_id": statement_document_id,
                    "statement_job_id": statement_job_id,
                    "statement_type": statement_document.get("document_type"),
                    "learned_layout_document_ids": learned_layout_ids,
                    "adaptive_layout_document_id": adaptive_upload["document_id"],
                    "adaptive_layout_profile_id": adaptive_layout.get("matched_profile_id"),
                    "adaptive_layout_score": adaptive_layout.get("score"),
                    "status": "PASS",
                },
                indent=2,
            )
        )


if __name__ == "__main__":
    main()
