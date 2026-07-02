# syntax=docker/dockerfile:1
FROM cgr.dev/chainguard/wolfi-base AS base

RUN apk upgrade --no-cache && apk add --no-cache go-1.26 make git curl

FROM base AS providers-builder
WORKDIR /boeing-providers/providers
COPY . /boeing-providers/providers
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/root/go/pkg/mod \
    BIN_DIR=/bin make package-providers && \
    mkdir -p /providers-runtime/boeing-providers/providers && \
    cp -a /boeing-providers/.envrc.providers.providers /providers-runtime/boeing-providers/ && \
    cp -a /boeing-providers/providers/auth-providers /providers-runtime/boeing-providers/providers/ && \
    cp -a /boeing-providers/providers/model-providers /providers-runtime/boeing-providers/providers/ && \
    for bin_dir in /boeing-providers/providers/*-provider/bin; do \
        provider_dir="$(dirname "${bin_dir}")"; \
        dest="/providers-runtime/boeing-providers/providers/$(basename "${provider_dir}")"; \
        mkdir -p "${dest}"; \
        cp -a "${bin_dir}" "${dest}/"; \
    done

FROM base AS providers
WORKDIR /boeing-providers/providers
COPY --from=providers-builder /providers-runtime/boeing-providers/ /boeing-providers/

FROM base AS encryption-bins-builder
WORKDIR /boeing-providers
COPY ./Makefile /boeing-providers/
COPY ./scripts/package-encryption-bins.sh /boeing-providers/scripts/

RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/root/go/pkg/mod \
    BIN_DIR=/boeing-providers/bin make package-encryption-bins && \
    mkdir -p /encryption-bins-runtime/bin /encryption-bins-runtime/boeing-providers && \
    cp -a /boeing-providers/.envrc.providers.encryption-bins /encryption-bins-runtime/boeing-providers/ && \
    cp -a /boeing-providers/bin/aws-encryption-provider /encryption-bins-runtime/bin/ && \
    cp -a /boeing-providers/bin/azure-encryption-provider /encryption-bins-runtime/bin/ && \
    cp -a /boeing-providers/bin/gcp-encryption-provider /encryption-bins-runtime/bin/

FROM base AS encryption-bins
WORKDIR /boeing-providers
COPY --from=encryption-bins-builder /encryption-bins-runtime/bin/ /bin/
COPY --from=encryption-bins-builder /encryption-bins-runtime/boeing-providers/ /boeing-providers/
