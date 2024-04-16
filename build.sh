#!/bin/bash

# This script will build all of the artifacts involved in a new Constellation daemon release.

# =================
# === Functions ===
# =================

# Print a failure message to stderr and exit
fail() {
    MESSAGE=$1
    RED='\033[0;31m'
    RESET='\033[;0m'
    >&2 echo -e "\n${RED}**ERROR**\n$MESSAGE${RESET}\n"
    exit 1
}


# Builds the Constellation daemon image and pushes it to Docker Hub
# NOTE: You must install qemu first; e.g. sudo apt-get install -y qemu qemu-user-static
build_daemon() {
    echo "Building Constellation daemon binaries..."
    docker buildx build --rm --platform=linux/amd64,linux/arm64 -f docker/daemon-build.dockerfile --output build/$VERSION --target daemon . || fail "Error building Constellation daemon binaries."
    echo "done!"

    # Flatted the folders to make it easier to upload artifacts to github
    mv build/$VERSION/linux_amd64/hyperdrive-constellation-daemon build/$VERSION/hyperdrive-constellation-daemon-linux-amd64
    mv build/$VERSION/linux_arm64/hyperdrive-constellation-daemon build/$VERSION/hyperdrive-constellation-daemon-linux-arm64

    # Clean up the empty directories
    rmdir build/$VERSION/linux_amd64 build/$VERSION/linux_arm64

    echo "Building Constellation Docker image..."
    # If uploading, make and push a manifest
    if [ "$UPLOAD" = true ]; then
        docker buildx build --rm --platform=linux/amd64,linux/arm64 --build-arg BINARIES_PATH=build/$VERSION -t nodeset/hyperdrive-constellation:$VERSION -f docker/daemon.dockerfile --push . || fail "Error building Constellation Docker image."
    else
        docker buildx build --rm --load --build-arg BINARIES_PATH=build/$VERSION -t nodeset/hyperdrive-constellation:$VERSION -f docker/daemon.dockerfile . || fail "Error building Constellation Docker image."
    fi
    echo "done!"
}


# Tags the 'latest' Docker Hub image
tag_latest() {
    echo -n "Tagging 'latest' Docker image... "
    docker tag nodeset/hyperdrive-constellation:$VERSION nodeset/hyperdrive-constellation:latest
    echo "done!"

    if [ "$UPLOAD" = true ]; then
        echo -n "Pushing to Docker Hub... "
        docker push nodeset/hyperdrive-constellation:latest
        echo "done!"
    else
        echo "The image tag only exists locally. Rerun with -u to upload to Docker Hub."
    fi
}


# Print usage
usage() {
    echo "Usage: build.sh [options] -v <version number>"
    echo "This script assumes it is in the hyperdrive-constellation repository directory."
    echo "Options:"
    echo $'\t-a\tBuild all of the artifacts'
    echo $'\t-d\tBuild the Hyperdrive Constellation daemon image and Docker container'
    echo $'\t-l\tTag the given version as "latest" on Docker Hub'
    echo $'\t-u\tWhen passed with a build, upload the resulting image tags to Docker Hub'
    exit 0
}


# =================
# === Main Body ===
# =================

# Parse arguments
while getopts "adluv:" FLAG; do
    case "$FLAG" in
        a) DAEMON=true ;;
        d) DAEMON=true ;;
        l) LATEST=true ;;
        u) UPLOAD=true ;;
        v) VERSION="$OPTARG" ;;
        *) usage ;;
    esac
done
if [ -z "$VERSION" ]; then
    usage
fi

# Cleanup old artifacts
rm -rf build/$VERSION/*
mkdir -p build/$VERSION

# Make a multiarch builder, ignore if it's already there
docker buildx create --name multiarch-builder --driver docker-container --use > /dev/null 2>&1

# Build the artifacts
if [ "$DAEMON" = true ]; then
    build_daemon
fi
if [ "$LATEST" = true ]; then
    tag_latest
fi