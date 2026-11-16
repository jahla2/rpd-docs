from app.extraction.financial import extract_financial_document


def block(block_id: str, text: str, confidence: float = 0.99):
    return {
        "block_id": block_id,
        "text": text,
        "bbox": [10, 10, 200, 30],
        "confidence": confidence,
    }


def test_supplier_statement_schema_extraction():
    html = """
    <table>
      <tr><th>Date</th><th>Invoice No</th><th>Debit</th><th>Credit</th><th>Balance</th></tr>
      <tr><td>2026-11-10</td><td>INV-9</td><td>1500.00</td><td></td><td>6500.00</td></tr>
    </table>
    """
    result = extract_financial_document(
        "doc-statement",
        "supplier_statement",
        [{
            "page": 1,
            "ocr_blocks": [
                block("b1", "Statement of Account"),
                block("b2", "Supplier: ABC Trading"),
                block("b3", "Statement Date: 2026-11-30"),
                block("b4", "Currency: PHP"),
                block("b5", "Previous Balance: 5000.00"),
                block("b6", "Current Balance: 6500.00"),
            ],
            "table_html": html,
        }],
    )

    assert result["document_type"] == "supplier_statement"
    assert result["fields"]["supplier_name"]["value"] == "ABC Trading"
    assert result["fields"]["statement_date"]["value"] == "2026-11-30"
    assert result["fields"]["closing_balance"]["value"] == "6500.00"
    assert result["fields"]["closing_balance"]["evidence"]["bbox"]
    assert result["tables"]["transactions"][0]["invoice_number"] == "INV-9"
    assert result["tables"]["transactions"][0]["balance"] == "6500.00"
    assert result["requires_review"] is False


def test_bank_statement_schema_extraction():
    result = extract_financial_document(
        "doc-bank",
        "bank_statement",
        [{
            "page": 1,
            "ocr_blocks": [
                block("b1", "BANK STATEMENT"),
                block("b2", "Bank: Metro Bank"),
                block("b3", "Account Number: 00123456"),
                block("b4", "Statement Period: 2026-11-01 - 2026-11-30"),
                block("b5", "Currency: PHP"),
                block("b6", "Opening Balance: 25000.00"),
                block("b7", "Closing Balance: 28000.00"),
            ],
            "table_html": None,
        }],
    )

    assert result["document_type"] == "bank_statement"
    assert result["fields"]["institution_name"]["value"] == "Metro Bank"
    assert result["fields"]["account_number"]["value"] == "00123456"
    assert result["fields"]["period_start"]["value"] == "2026-11-01"
    assert result["fields"]["period_end"]["value"] == "2026-11-30"
    assert result["fields"]["closing_balance"]["value"] == "28000.00"
    assert result["requires_review"] is False


def test_invoice_routes_to_existing_invoice_extractor():
    result = extract_financial_document(
        "doc-invoice",
        "invoice",
        [{
            "page": 1,
            "ocr_blocks": [
                block("b1", "ABC Trading"),
                block("b2", "Invoice Number: INV-001"),
                block("b3", "Invoice Date: 2026-11-16"),
                block("b4", "Currency PHP"),
                block("b5", "Grand Total: 1120.00"),
            ],
            "table_html": None,
        }],
    )
    assert result["document_type"] == "invoice"
    assert result["invoice_number"]["value"] == "INV-001"
