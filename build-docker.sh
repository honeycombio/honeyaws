set -o nounset
set -o pipefail
set -o xtrace

TAGS="latest"
VERSION="dev"
if [[ -n ${CIRCLE_TAG:-} ]]; then
    # trim 'v' prefix if present
    VERSION=${CIRCLE_TAG#"v"}
    # append version to image tags
    TAGS+=",$VERSION"
fi

unset GOOS
unset GOARCH
export KO_DOCKER_REPO="ko.local"
export GOFLAGS="-ldflags=-X=main.BuildID=$VERSION"
export SOURCE_DATE_EPOCH=$(date +%s)

# shellcheck disable=SC2086
for NAME in honeyalb honeycloudfront honeycloudtrail honeyelb;
do
  ko publish \
    --tags "${TAGS}" \
    --base-import-paths \
    --platform "linux/amd64,linux/arm64" \
    ./cmd/$NAME

  # update tags to use correct org name
  for TAG in ${TAGS//,/ }
  do
    docker image tag ko.local/$NAME honeycombio/$NAME:$TAG
  done
done
