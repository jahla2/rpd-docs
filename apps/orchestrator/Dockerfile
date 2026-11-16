FROM golang:1.23-bookworm AS build

RUN apt-get update && apt-get install -y --no-install-recommends \
    cmake g++ libopencv-dev poppler-utils \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src
COPY apps/orchestrator ./apps/orchestrator
COPY apps/native-engine ./apps/native-engine

WORKDIR /src/apps/orchestrator
RUN go mod download
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/rpd-orchestrator ./cmd/worker

WORKDIR /src
RUN cmake -S apps/native-engine -B /tmp/native-build -DCMAKE_BUILD_TYPE=Release \
    && cmake --build /tmp/native-build --config Release -j2 \
    && cp /tmp/native-build/rpd-native-engine /out/rpd-native-engine

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates libopencv-dev poppler-utils \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/rpd-orchestrator /usr/local/bin/rpd-orchestrator
COPY --from=build /out/rpd-native-engine /usr/local/bin/rpd-native-engine

RUN useradd --system --uid 10001 --create-home rpd \
    && mkdir -p /data/storage \
    && chown -R rpd:rpd /data

USER rpd
WORKDIR /app

ENTRYPOINT ["/usr/local/bin/rpd-orchestrator"]
