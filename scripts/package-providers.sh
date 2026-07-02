#!/bin/bash
set -e -x -o pipefail

REPO=github.com/boeing-ai-gateway/providers
REPO_DIR=/boeing-providers/providers
REPO_NAME=$(basename $REPO_DIR)

if [[ -x "${REPO_DIR}/scripts/build.sh" ]]; then
    (
        echo "Running build script for ${REPO}..."
        cd "${REPO_DIR}"
        ./scripts/build.sh
        echo "Build script for ${REPO} complete!"
    )
else
    echo "No build script found in ${REPO}"
fi

BOEING_SERVER_VERSIONS="$(
    cat <<VERSIONS
${REPO}=$(cd /boeing-providers/providers && git rev-parse --short HEAD),${BOEING_SERVER_VERSIONS}
VERSIONS
)"
BOEING_SERVER_VERSIONS="${BOEING_SERVER_VERSIONS%,}"

cd /boeing-providers
cat <<EOF >.envrc.providers.${REPO_NAME}
export BOEING_SERVER_PROVIDER_REGISTRIES="/boeing-providers/providers"
export BOEING_SERVER_VERSIONS="${BOEING_SERVER_VERSIONS}"
EOF
