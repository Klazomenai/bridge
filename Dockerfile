FROM golang:1.25 AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0: pure-Go binary, distroless-compatible.
# -tags goolm: pure-Go olm implementation (no libolm shared library needed).
RUN CGO_ENABLED=0 go build -tags goolm -trimpath \
    -ldflags="-s -w" \
    -o /out/bridge ./cmd/bridge

FROM gcr.io/distroless/static:nonroot

# Crew registry config is mounted or baked in at runtime.
COPY --from=builder /out/bridge /bridge
COPY config/crew.yaml /config/crew.yaml

# The crypto store PVC is mounted at /data/crypto-store at runtime.
# The Anthropic API key is mounted at /run/secrets/anthropic/api_key at runtime.
USER nonroot

ENTRYPOINT ["/bridge"]
