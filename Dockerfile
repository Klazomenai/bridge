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

# Non-root user — UID/GID 65532 matches existing deployment securityContext
# (runAsUser, runAsGroup, fsGroup) so no Helm chart changes needed.
RUN addgroup -g 65532 bridge && \
    adduser -D -u 65532 -G bridge bridge

# kubectl and helm — pinned versions and SHA256 hashes for supply-chain integrity.
# Hashes are verified locally; no trust placed on upstream checksum endpoints.
# curl removed after install to minimise attack surface.
ARG KUBECTL_VERSION=v1.33.4
ARG KUBECTL_SHA256=c2ba72c115d524b72aaee9aab8df8b876e1596889d2f3f27d68405262ce86ca1
ARG HELM_VERSION=v3.11.1
ARG HELM_SHA256=0b1be96b66fab4770526f136f5f1a385a47c41923d33aab0dcb500e0f6c1bf7c
RUN apk add --no-cache ca-certificates curl && \
    curl -fsSL "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" \
      -o /tmp/kubectl && \
    echo "${KUBECTL_SHA256}  /tmp/kubectl" | sha256sum -c - && \
    install -m 0755 /tmp/kubectl /usr/local/bin/kubectl && \
    curl -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-amd64.tar.gz" \
      -o /tmp/helm.tar.gz && \
    echo "${HELM_SHA256}  /tmp/helm.tar.gz" | sha256sum -c - && \
    tar xzf /tmp/helm.tar.gz -C /usr/local/bin --strip-components=1 linux-amd64/helm && \
    rm -f /tmp/kubectl /tmp/helm.tar.gz && \
    apk del curl

# Same directory structure as previous distroless build — kubelet mounts
# volumes over these paths at runtime.
RUN install -d -m 0700 -o bridge -g bridge /var/lib/bridge && \
    mkdir -p /run/secrets/anthropic

COPY --from=builder /out/bridge /bridge
COPY config/crew.yaml /config/crew.yaml

USER bridge

ENTRYPOINT ["/bridge"]
