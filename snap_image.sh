#!/usr/bin/env bash

set -euo pipefail

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )/../"

ORIGIN=${ORIGIN:-origin}

REPO_NAME=${REPO_NAME:-doctl-snap-base}
LATEST="${REPO_NAME}:latest"
SAMMY_LATEST="sammytheshark/${LATEST}"

repo_missing() {
  [[ $(docker images --format "{{.ID}}" "$1" | wc -l) -eq 0 ]]
}

build_local_snap() {
  docker tag "$REPO_NAME" "sammytheshark/$REPO_NAME"
  cd "$DIR" && sudo make clean
  make _build_snap
}

build_pre() {
  version="$(ORIGIN=${ORIGIN} SNAP_IMAGE=true "$DIR/scripts/version.sh")"
  prerelease="sammytheshark/${REPO_NAME}:${version}"

  docker tag "$REPO_NAME" "$prerelease" && \
    docker push "$prerelease"

  docker tag "$REPO_NAME" "$SAMMY_LATEST" && \
    docker push "$SAMMY_LATEST"
}

build_finalize() {
  version=$(ORIGIN="${ORIGIN}" "$DIR/scripts/version.sh" -s)
  release="sammytheshark/${REPO_NAME}:$version"

  docker tag "$SAMMY_LATEST" "$release" && \
    docker push "$release"
}

build_snap_image() {
  < "$DIR/dockerfiles/Dockerfile.snap" docker build --no-cache -t "$REPO_NAME" -

  cat <<INSTRUCTIONS
######################################################################
######################################################################

Congrats! You built a new docker image for building snaps!

Now you need to:
A. Test your image
B. Push a prerelease version of that image to Dockerhub
C. Release a new version of doctl with any changes, e.g., to Dockerfile.snap
D. Rename the prerelease image to a released version.

Step by step, in detail:

###
A: Test your image

1. Build a local snap from your image: make build_local_snap

3. install the resulting snap locally

   sudo snap install doctl_vX.XX.XXX*.snap --dangerous
   sudo snap connect doctl:doctl-config
   sudo snap connect doctl:kube-config

4. take it for a spin.

###
B. Push a prerelease version of that image to Dockerhub

Login to dockerhub as sammytheshark (credentials in LastPass) and
tag and push the image using the make target

   docker login -u sammytheshark -p <from LastPass>
   make prerelease_snap_image

Check your work:
- Visit https://hub.docker.com/repository/registry-1.docker.io/sammytheshark/doctl-snap-base/tags?page=1
- Check the last updated date on the image tagged 'latest'

###
C. Release a new version of doctl with any changes

If the only change is a new snap image, fixing a broken snap build,
that's a patch, by the way. :)

###
D. Rename the prerelease image to the new released version.

make finalize_snap_image

######################################################################
######################################################################
INSTRUCTIONS
}

hash docker 2>/dev/null ||
  { echo >&2 "I require the docker CLI but it's not installed. Please see https://docs.docker.com/install/. Aborting."; exit 1; }

BUILD=${BUILD:-snap_image}

case "$BUILD" in
  local_snap | pre)
    if repo_missing "$LATEST"; then
      echo "Could not find ${LATEST}. Did you run 'make snap_image'?"
      exit 1
    fi
    ;;
  
  finalize)
    if repo_missing "$SAMMY_LATEST"; then
      echo "Could not find ${SAMMY_LATEST}. Did you run 'make prerelease_snap_image'?"
      exit 1
    fi
    ;;
esac

"build_${BUILD}"
