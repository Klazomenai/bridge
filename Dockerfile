FROM golang:1.25 AS builder

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0: pure-Go static binary (no libc dependency).
# -tags goolm: pure-Go olm implementation (no libolm shared library needed).
RUN CGO_ENABLED=0 go build -tags goolm -trimpath \
    -ldflags="-s -w" \
    -o /out/bridge ./cmd/bridge

# Alpine runtime — kubectl and helm needed for Maren/Bosun cluster tools.
# Distroless has no package manager or shell; Alpine is minimal (~7MB) and
# version-pinned per security skill guidance.
FROM alpine:3.21

# Non-root user — UID 65532 matches existing deployment securityContext
# (runAsUser, runAsGroup, fsGroup) so no Helm chart changes needed.
RUN adduser -D -u 65532 -g 65532 bridge

# kubectl and helm — pinned versions, compatible with GKE RAPID channel.
# curl removed after install to minimise attack surface.
ARG KUBECTL_VERSION=v1.33.4
ARG HELM_VERSION=v3.11.1
RUN apk add --no-cache ca-certificates curl && \
    curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" \
      -o /usr/local/bin/kubectl && chmod +x /usr/local/bin/kubectl && \
    curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz" | \
      tar xz -C /usr/local/bin --strip-components=1 linux-amd64/helm && \
    apk del curl

# Same directory structure as previous distroless build — kubelet mounts
# volumes over these paths at runtime.
RUN install -d -m 0700 -o bridge -g bridge /var/lib/bridge && \
    mkdir -p /run/secrets/anthropic

COPY --from=builder /out/bridge /bridge
COPY config/crew.yaml /config/crew.yaml

USER bridge

ENTRYPOINT ["/bridge"]
