from app.extraction.invoice import extract_line_items


def test_custom_mapping_alias_and_normalize_operation():
    html = """
    <table>
      <tr><th>Particular Name</th><th>Units Sold</th><th>Extended Value</th></tr>
      <tr><td>  Paper   Box </td><td>2</td><td>1,250.50</td></tr>
    </table>
    """
    config = {
        "columns": [
            {
                "source_aliases": ["Particular Name"],
                "target": "description",
                "type": "string",
                "operations": [{"operation": "normalize"}],
            },
            {
                "source_aliases": ["Units Sold"],
                "target": "quantity",
                "type": "decimal",
                "operations": [{"operation": "decimal"}],
            },
            {
                "source_aliases": ["Extended Value"],
                "target": "line_total",
                "type": "decimal",
                "operations": [{"operation": "decimal"}],
            },
        ]
    }
    items, meta = extract_line_items(html, config)
    assert meta["mapped_headers"]["0"] == "description"
    assert items[0]["description"] == "Paper Box"
    assert items[0]["quantity"] == "2"
    assert items[0]["line_total"] == "1250.50"



def test_split_operation_is_literal_bounded_and_trimmed():
    html = """
    <table>
      <tr><th>Item Ref</th><th>Line Total</th></tr>
      <tr><td>SKU- 001 </td><td>100.00</td></tr>
    </table>
    """
    config = {
        "columns": [
            {
                "source_aliases": ["Item Ref"],
                "target": "item_code",
                "type": "identifier",
                "operations": [
                    {
                        "operation": "split",
                        "config": {"delimiter": "-", "index": 1, "trim": True},
                    }
                ],
            },
            {
                "source_aliases": ["Line Total"],
                "target": "line_total",
                "type": "decimal",
                "operations": [{"operation": "decimal"}],
            },
        ]
    }
    items, _ = extract_line_items(html, config)
    assert items[0]["item_code"] == "001"


def test_split_operation_missing_part_fails_closed():
    html = """
    <table>
      <tr><th>Item Ref</th><th>Line Total</th></tr>
      <tr><td>SKU-001</td><td>100.00</td></tr>
    </table>
    """
    config = {
        "columns": [
            {
                "source_aliases": ["Item Ref"],
                "target": "item_code",
                "type": "identifier",
                "operations": [
                    {
                        "operation": "split",
                        "config": {"delimiter": "-", "index": 3, "trim": True},
                    }
                ],
            },
            {
                "source_aliases": ["Line Total"],
                "target": "line_total",
                "type": "decimal",
                "operations": [{"operation": "decimal"}],
            },
        ]
    }
    items, _ = extract_line_items(html, config)
    assert "item_code" not in items[0]



def test_split_can_feed_multiple_targets_from_one_source_column():
    html = """
    <table>
      <tr><th>Composite Ref</th><th>Line Total</th></tr>
      <tr><td>SUP001-INV100</td><td>100.00</td></tr>
    </table>
    """
    config = {
        "columns": [
            {
                "source_aliases": ["Composite Ref"],
                "target": "supplier_code",
                "type": "identifier",
                "operations": [
                    {
                        "operation": "split",
                        "config": {"delimiter": "-", "index": 0, "trim": True},
                    }
                ],
            },
            {
                "source_aliases": ["Composite Ref"],
                "target": "invoice_number",
                "type": "identifier",
                "operations": [
                    {
                        "operation": "split",
                        "config": {"delimiter": "-", "index": 1, "trim": True},
                    }
                ],
            },
            {
                "source_aliases": ["Line Total"],
                "target": "line_total",
                "type": "decimal",
                "operations": [{"operation": "decimal"}],
            },
        ]
    }
    items, meta = extract_line_items(html, config)
    assert meta["mapped_headers"]["0"] == ["supplier_code", "invoice_number"]
    assert items[0]["supplier_code"] == "SUP001"
    assert items[0]["invoice_number"] == "INV100"
    assert items[0]["line_total"] == "100.00"
