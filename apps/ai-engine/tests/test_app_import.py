from app.main import app


def test_fastapi_application_imports_and_registers_core_routes() -> None:
    routes = {getattr(route, "path", "") for route in app.routes}

    expected = {
        "/health/live",
        "/health/ready",
        "/v1/ocr",
        "/v1/classify",
        "/v1/pdftext",
        "/v1/tables",
        "/v1/extract/invoice",
        "/v1/extract/invoice-document",
        "/v1/agent/resolve-invoice",
    }

    assert expected.issubset(routes)
