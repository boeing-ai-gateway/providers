#!/bin/bash
set -e -x -o pipefail

BIN_DIR=${BIN_DIR:-./bin}

cd /boeing-providers

if [ ! -e aws-encryption-provider ]; then
    git clone --depth=1 https://github.com/boeing-ai-gateway/aws-encryption-provider
fi
cd /boeing-providers/aws-encryption-provider
go build -o "${BIN_DIR}/aws-encryption-provider" cmd/server/main.go
BOEING_SERVER_VERSIONS="$(
    cat <<VERSIONS
github.com/boeing-ai-gateway/aws-encryption-provider=$(git rev-parse --short HEAD),${BOEING_SERVER_VERSIONS}
VERSIONS
)"

cd /boeing-providers

if [ ! -e kubernetes-kms ]; then
    git clone --depth=1 https://github.com/boeing-ai-gateway/kubernetes-kms
fi
cd /boeing-providers/kubernetes-kms
go build -ldflags="-s -w" -o "${BIN_DIR}/azure-encryption-provider" cmd/server/main.go
BOEING_SERVER_VERSIONS="$(
    cat <<VERSIONS
github.com/boeing-ai-gateway/kubernetes-kms=$(git rev-parse --short HEAD),${BOEING_SERVER_VERSIONS}
VERSIONS
)"
BOEING_SERVER_VERSIONS="${BOEING_SERVER_VERSIONS%,}"

cd /boeing-providers

if [ ! -e k8s-cloudkms-plugin ]; then
    git clone --depth=1 https://github.com/boeing-ai-gateway/k8s-cloudkms-plugin
fi
cd /boeing-providers/k8s-cloudkms-plugin
go build -ldflags "-s -w -extldflags 'static'" -installsuffix cgo -tags netgo -o "${BIN_DIR}/gcp-encryption-provider" cmd/k8s-cloudkms-plugin/main.go
BOEING_SERVER_VERSIONS="$(
    cat <<VERSIONS
github.com/boeing-ai-gateway/k8s-cloudkms-plugin=$(git rev-parse --short HEAD),${BOEING_SERVER_VERSIONS}
VERSIONS
)"

cd /boeing-providers
cat <<EOF >.envrc.providers.encryption-bins
export BOEING_SERVER_VERSIONS="${BOEING_SERVER_VERSIONS}"
EOF
