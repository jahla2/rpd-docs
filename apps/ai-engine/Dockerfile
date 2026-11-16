FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1

RUN apt-get update && apt-get install -y --no-install-recommends \
    libgl1 libglib2.0-0 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY apps/ai-engine/pyproject.toml ./pyproject.toml
COPY apps/ai-engine/app ./app

RUN python -m pip install --upgrade pip \
    && pip install -e ".[ocr]"

# Download/initialize OCR and table model assets while the image is built as
# root. Runtime containers stay non-root and never need to write site-packages.
RUN python -c "from app.ocr.rapidocr_adapter import RapidOCREngine; RapidOCREngine(); from app.tables.slanet_adapter import SLANetTableEngine; SLANetTableEngine(); print('RapidOCR and SLANet+ models ready')"

RUN useradd --system --uid 10001 --create-home rpd \
    && mkdir -p /data/storage \
    && chown -R rpd:rpd /data

USER rpd
EXPOSE 8000

CMD ["uvicorn","app.main:app","--host","0.0.0.0","--port","8000"]
