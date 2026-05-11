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

# Stage to fetch klazomenai/dotfiles-sourced skill content at a pinned SHA.
# The bridge binary reads its skill content from the embedded fallback
# (compiled into the binary via internal/crew/skills/embedded/); this
# image-baked copy gives operators an inspectable surface
# (`kubectl exec -- cat /var/lib/klazomenai/skills/_universal.md`) that
# does not require grovelling the binary.
#
# DOTFILES_REF must be a full 40-char git SHA on klazomenai/dotfiles
# main. Bump ceremony: see CONTRIBUTING.md "Bumping `DOTFILES_REF`".
# The skills-drift CI workflow catches DOTFILES_REF bumps not paired
# with a re-bundle of internal/crew/skills/embedded/.
FROM alpine:3.21 AS dotfiles

ARG DOTFILES_REF=4b856162d85d147648a26fb3fd5573a0f9b7e15d

RUN apk add --no-cache git ca-certificates

WORKDIR /dotfiles
# Fetch by SHA directly; --depth=1 keeps the stage small. GitHub
# permits arbitrary-SHA fetches on its hosted repos
# (uploadpack.allowAnySHA1InWant), so no separate clone-then-checkout
# round-trip is needed.
RUN git init -q && \
    git remote add origin https://github.com/Klazomenai/dotfiles && \
    git fetch -q --depth=1 origin "${DOTFILES_REF}" && \
    git checkout -q FETCH_HEAD

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

# Bake skill content from the pinned dotfiles ref. The orchestrator
# reads the embedded fallback (compiled into the binary) at runtime;
# this image-baked copy is the operator-inspectable surface. The
# skills-drift CI workflow catches drift between the embedded fallback
# and the dotfiles ref these files come from.
COPY --from=dotfiles /dotfiles/claude/profiles/_universal.md /var/lib/klazomenai/skills/_universal.md
COPY --from=dotfiles /dotfiles/claude/skills/github/SKILL.md /var/lib/klazomenai/skills/github/SKILL.md
COPY --from=dotfiles /dotfiles/claude/profiles/github.md /var/lib/klazomenai/skills/github/profile.md
# `find ... -name '*.md'` recurses correctly into the github/ subdir;
# the AC's `chmod -R 0444 .../*.md` form would only match the top-level
# _universal.md because of shell-glob expansion semantics.
RUN chown -R bridge:bridge /var/lib/klazomenai/skills && \
    find /var/lib/klazomenai/skills -name '*.md' -exec chmod 0444 {} +

USER bridge

ENTRYPOINT ["/bridge"]
