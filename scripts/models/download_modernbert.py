from __future__ import annotations

import argparse
import json
import os
import shutil
import urllib.request
from pathlib import Path


MODEL_REPO = "MoritzLaurer/ModernBERT-base-zeroshot-v2.0"
MODEL_API = f"https://huggingface.co/api/models/{MODEL_REPO}"
FILES = (
    "config.json",
    "tokenizer.json",
    "tokenizer_config.json",
    "special_tokens_map.json",
    "onnx/model_int8.onnx",
)


def resolve_revision(requested: str) -> str:
    if requested and requested != "main":
        return requested
    request = urllib.request.Request(
        MODEL_API,
        headers={"User-Agent": "rpd-docs-model-downloader/1.0"},
    )
    with urllib.request.urlopen(request, timeout=30) as response:
        payload = json.load(response)
    revision = str(payload.get("sha") or "").strip()
    if not revision:
        raise RuntimeError("Hugging Face model metadata did not include a revision SHA")
    return revision


def download_file(revision: str, remote_path: str, destination: Path, force: bool) -> None:
    if destination.is_file() and destination.stat().st_size > 0 and not force:
        print(f"[skip] {destination}")
        return

    destination.parent.mkdir(parents=True, exist_ok=True)
    temporary = destination.with_suffix(destination.suffix + ".part")
    url = f"https://huggingface.co/{MODEL_REPO}/resolve/{revision}/{remote_path}?download=true"
    request = urllib.request.Request(
        url,
        headers={"User-Agent": "rpd-docs-model-downloader/1.0"},
    )

    print(f"[download] {remote_path}")
    try:
        with urllib.request.urlopen(request, timeout=120) as response, temporary.open("wb") as handle:
            shutil.copyfileobj(response, handle, length=1024 * 1024)
        os.replace(temporary, destination)
    finally:
        if temporary.exists():
            temporary.unlink()


def main() -> None:
    parser = argparse.ArgumentParser(description="Download the local ModernBERT INT8 classifier")
    parser.add_argument("--output", default="models/modernbert")
    parser.add_argument("--revision", default="main")
    parser.add_argument("--force", action="store_true")
    args = parser.parse_args()

    output = Path(args.output).resolve()
    revision = resolve_revision(args.revision)
    print(f"Model: {MODEL_REPO}")
    print(f"Revision: {revision}")
    print(f"Output: {output}")

    for remote_path in FILES:
        download_file(revision, remote_path, output / remote_path, args.force)

    manifest = {
        "model": MODEL_REPO,
        "revision": revision,
        "files": list(FILES),
        "runtime": "onnxruntime",
        "default_model_file": "onnx/model_int8.onnx",
    }
    (output / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    print("[done] ModernBERT local model is ready")


if __name__ == "__main__":
    main()
