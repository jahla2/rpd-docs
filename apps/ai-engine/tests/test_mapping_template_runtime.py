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



def test_combine_operation_uses_same_table_row_context():
    html = """
    <table>
      <tr><th>Item Name</th><th>Variant</th><th>Line Total</th></tr>
      <tr><td>Paper</td><td>Blue</td><td>100.00</td></tr>
    </table>
    """
    config = {
        "columns": [
            {
                "source_aliases": ["Item Name"],
                "target": "description",
                "type": "string",
                "operations": [
                    {
                        "operation": "combine",
                        "config": {
                            "with": ["Variant"],
                            "separator": " / ",
                            "skip_empty": False,
                        },
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
    assert items[0]["description"] == "Paper / Blue"
    assert items[0]["line_total"] == "100.00"


def test_combine_operation_missing_required_source_fails_closed():
    html = """
    <table>
      <tr><th>Item Name</th><th>Line Total</th></tr>
      <tr><td>Paper</td><td>100.00</td></tr>
    </table>
    """
    config = {
        "columns": [
            {
                "source_aliases": ["Item Name"],
                "target": "description",
                "type": "string",
                "operations": [
                    {
                        "operation": "combine",
                        "config": {
                            "with": ["Variant"],
                            "separator": " / ",
                            "skip_empty": False,
                        },
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
    assert "description" not in items[0]
    assert items[0]["line_total"] == "100.00"



def test_compute_operation_calculates_line_total_from_same_row():
    html = """
    <table>
      <tr><th>Description</th><th>Quantity</th><th>Unit Price</th><th>Line Total</th></tr>
      <tr><td>Office Supplies</td><td>2</td><td>500.00</td><td>1000.00</td></tr>
    </table>
    """
    config = {
        "columns": [
            {
                "source_aliases": ["Quantity"],
                "target": "calculated_line_total",
                "type": "decimal",
                "operations": [
                    {
                        "operation": "compute",
                        "config": {
                            "operator": "multiply",
                            "operand": "Unit Price",
                            "scale": 2,
                        },
                    }
                ],
            }
        ]
    }
    items, _ = extract_line_items(html, config)
    assert items[0]["quantity"] == "2"
    assert items[0]["unit_price"] == "500.00"
    assert items[0]["line_total"] == "1000.00"
    assert items[0]["calculated_line_total"] == "1000.00"


def test_compute_operation_missing_operand_fails_closed():
    html = """
    <table>
      <tr><th>Description</th><th>Quantity</th><th>Line Total</th></tr>
      <tr><td>Office Supplies</td><td>2</td><td>1000.00</td></tr>
    </table>
    """
    config = {
        "columns": [
            {
                "source_aliases": ["Quantity"],
                "target": "calculated_line_total",
                "type": "decimal",
                "operations": [
                    {
                        "operation": "compute",
                        "config": {
                            "operator": "multiply",
                            "operand": "Unit Price",
                            "scale": 2,
                        },
                    }
                ],
            }
        ]
    }
    items, _ = extract_line_items(html, config)
    assert "calculated_line_total" not in items[0]



def test_lookup_operation_never_leaks_raw_value_from_ai_runtime():
    html = """
    <table>
      <tr><th>Supplier Name</th><th>Line Total</th></tr>
      <tr><td>ABC Trading</td><td>100.00</td></tr>
    </table>
    """
    config = {
        "columns": [
            {
                "source_aliases": ["Supplier Name"],
                "target": "supplier_id",
                "type": "identifier",
                "operations": [
                    {
                        "operation": "lookup",
                        "config": {
                            "registry": "supplier_master",
                            "source_field": "supplier_name",
                            "on_missing": "review",
                        },
                    }
                ],
            }
        ]
    }
    items, _ = extract_line_items(html, config)
    assert "supplier_id" not in items[0]
    assert items[0]["line_total"] == "100.00"
