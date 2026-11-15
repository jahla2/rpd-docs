from pathlib import Path

from PIL import Image

from app.pdftext.poppler import parse_bbox_layout


def test_parse_poppler_bbox_and_scale(tmp_path: Path):
    layout = tmp_path / "page.html"
    layout.write_text(
        """<html><body><doc><page width="100" height="200">
        <flow><block><line xMin="10" yMin="20" xMax="90" yMax="40">
        <word xMin="10" yMin="20" xMax="30" yMax="40">Invoice</word>
        <word xMin="35" yMin="20" xMax="90" yMax="40">INV-001</word>
        </line></block></flow></page></doc></body></html>""",
        encoding="utf-8",
    )
    image = tmp_path / "page.png"
    Image.new("L", (200, 400), color=255).save(image)

    result = parse_bbox_layout(str(layout), str(image), "doc-1", 2)
    assert result["engine"] == "poppler_text"
    assert result["page"] == 2
    assert result["blocks"][0]["text"] == "Invoice INV-001"
    assert result["blocks"][0]["bbox"] == [20.0, 40.0, 180.0, 40.0, 180.0, 80.0, 20.0, 80.0]
