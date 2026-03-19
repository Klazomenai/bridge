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

# Create empty mount point directories. distroless/static is built from scratch
# and contains neither /data nor /run; kubelet mounts volumes over these paths
# at runtime. Without them in the image the behaviour is runtime-implementation-
# specific — creating them here makes the contract explicit and portable.
RUN mkdir -p /staging/var/lib/bridge /staging/run/secrets/anthropic

FROM gcr.io/distroless/static:nonroot

COPY --from=builder /out/bridge /bridge
COPY --from=builder /staging/var /var
COPY --from=builder /staging/run /run
COPY config/crew.yaml /config/crew.yaml

USER nonroot

ENTRYPOINT ["/bridge"]
