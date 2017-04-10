#!/usr/bin/env bash

# fail immediately
set -e
set -o pipefail

echo "Branch: $BITBUCKET_BRANCH"
echo "Tag: $BITBUCKET_TAG"
echo "Commit: $BITBUCKET_COMMIT"

APPNAME="amdatu-kubernetes-deployer"

ALPHA_LINUX_NAME="$APPNAME-linux_amd64-alpha"
ALPHA_MACOS_NAME="$APPNAME-macos_amd64-alpha"
ALPHA_WIN_NAME="$APPNAME-windows_amd64-alpha.exe"
HASHED_LINUX_NAME="$APPNAME-linux_amd64-${BITBUCKET_COMMIT}"
HASHED_MACOS_NAME="$APPNAME-macos_amd64-${BITBUCKET_COMMIT}"
HASHED_WIN_NAME="$APPNAME-windows_amd64-${BITBUCKET_COMMIT}.exe"

ALPHA_IMAGE="amdatu/${APPNAME}:alpha"
HASHED_IMAGE="amdatu/${APPNAME}:${BITBUCKET_COMMIT}"

if [ -z "$BITBUCKET_TAG" ]; then

    # build for every branch, but not on tags

    # save where we are coming from, that's where our sources are
    SRCDIR=`pwd`

    # create correct golang dir structure for this project
    echo "Creating directory structure"
    PACKAGE_PATH="${GOPATH}/src/bitbucket.org/amdatulabs/${APPNAME}"
    mkdir -p "${PACKAGE_PATH}"

    # copy sources to new directory
    echo "Copying sources"
    cd "${SRCDIR}"
    tar -cO --exclude .git . | tar -xv -C "${PACKAGE_PATH}"

    # build binaries
    echo "Building binaries"
    cd "${PACKAGE_PATH}"

    CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o "$ALPHA_LINUX_NAME" .
    CGO_ENABLED=0 GOOS=darwin go build -a -installsuffix cgo -o "$ALPHA_MACOS_NAME" .
    CGO_ENABLED=0 GOOS=windows go build -a -installsuffix cgo -o "$ALPHA_WIN_NAME" .

    echo "Build successful"

fi

if [ -z "$BITBUCKET_TAG" ] && [ "$BITBUCKET_BRANCH" == "master" ]; then

    # publish alpha versions for master branch

    if [ -n "$BB_AUTH_STRING" ]; then

        # upload binaries to download section of bitbucket

        echo "Uploading binaries"
        cp "$ALPHA_LINUX_NAME" "$HASHED_LINUX_NAME"
        cp "$ALPHA_MACOS_NAME" "$HASHED_MACOS_NAME"
        cp "$ALPHA_WIN_NAME" "$HASHED_WIN_NAME"

        curl -X POST "https://${BB_AUTH_STRING}@api.bitbucket.org/2.0/repositories/${BITBUCKET_REPO_OWNER}/${BITBUCKET_REPO_SLUG}/downloads" --form files=@"$ALPHA_LINUX_NAME"
        curl -X POST "https://${BB_AUTH_STRING}@api.bitbucket.org/2.0/repositories/${BITBUCKET_REPO_OWNER}/${BITBUCKET_REPO_SLUG}/downloads" --form files=@"$ALPHA_MACOS_NAME"
        curl -X POST "https://${BB_AUTH_STRING}@api.bitbucket.org/2.0/repositories/${BITBUCKET_REPO_OWNER}/${BITBUCKET_REPO_SLUG}/downloads" --form files=@"$ALPHA_WIN_NAME"
        curl -X POST "https://${BB_AUTH_STRING}@api.bitbucket.org/2.0/repositories/${BITBUCKET_REPO_OWNER}/${BITBUCKET_REPO_SLUG}/downloads" --form files=@"$HASHED_LINUX_NAME"
        curl -X POST "https://${BB_AUTH_STRING}@api.bitbucket.org/2.0/repositories/${BITBUCKET_REPO_OWNER}/${BITBUCKET_REPO_SLUG}/downloads" --form files=@"$HASHED_MACOS_NAME"
        curl -X POST "https://${BB_AUTH_STRING}@api.bitbucket.org/2.0/repositories/${BITBUCKET_REPO_OWNER}/${BITBUCKET_REPO_SLUG}/downloads" --form files=@"$HASHED_WIN_NAME"
    else
        echo "No bitbucket auth token set, skipping upload"
    fi

    if [ -n "$DOCKER_USER" ] && [ -n "$DOCKER_MAIL" ] && [ -n "$DOCKER_PASSWORD" ]; then

        # build docker image for alpha version

        echo "Building alpha docker image"
        cp "$ALPHA_LINUX_NAME" "$APPNAME"
        docker build -t "$ALPHA_IMAGE" .
        echo "Tagging hashed docker image"
        docker tag "$ALPHA_IMAGE" "$HASHED_IMAGE"
        echo "Pushing docker images"
        docker login --username="$DOCKER_USER" --email="$DOCKER_MAIL" --password="$DOCKER_PASSWORD"
        docker push "$ALPHA_IMAGE"
        docker push "$HASHED_IMAGE"
    else
        echo "No docker user/password set, skipping image build and push"
    fi

elif [ -n "$BITBUCKET_TAG" ]; then

    # create binaries and images versioned with given git tag
    # use existing hashed versions, do not rebuild!

    if [ -n "$BB_AUTH_STRING" ]; then

        # download old hashed version, rename, upload

        echo "Downloading old binaries"
        curl -sL "https://bitbucket.org/amdatulabs/${APPNAME}/downloads/${HASHED_LINUX_NAME}" -o "$HASHED_LINUX_NAME"
        curl -sL "https://bitbucket.org/amdatulabs/${APPNAME}/downloads/${HASHED_MACOS_NAME}" -o "$HASHED_MACOS_NAME"
        curl -sL "https://bitbucket.org/amdatulabs/${APPNAME}/downloads/${HASHED_WIN_NAME}" -o "$HASHED_WIN_NAME"

        TAGGED_LINUX_NAME="$APPNAME-linux_amd64-${BITBUCKET_TAG}"
        TAGGED_MACOS_NAME="$APPNAME-macos_amd64-${BITBUCKET_TAG}"
        TAGGED_WIN_NAME="$APPNAME-windows_amd64-${BITBUCKET_TAG}.exe"

        cp "$HASHED_LINUX_NAME" "$TAGGED_LINUX_NAME"
        cp "$HASHED_MACOS_NAME" "$TAGGED_MACOS_NAME"
        cp "$HASHED_WIN_NAME" "$TAGGED_WIN_NAME"

        echo "Uploading renamed binaries"
        curl -X POST "https://${BB_AUTH_STRING}@api.bitbucket.org/2.0/repositories/${BITBUCKET_REPO_OWNER}/${BITBUCKET_REPO_SLUG}/downloads" --form files=@"$TAGGED_LINUX_NAME"
        curl -X POST "https://${BB_AUTH_STRING}@api.bitbucket.org/2.0/repositories/${BITBUCKET_REPO_OWNER}/${BITBUCKET_REPO_SLUG}/downloads" --form files=@"$TAGGED_MACOS_NAME"
        curl -X POST "https://${BB_AUTH_STRING}@api.bitbucket.org/2.0/repositories/${BITBUCKET_REPO_OWNER}/${BITBUCKET_REPO_SLUG}/downloads" --form files=@"$TAGGED_WIN_NAME"
    else
        echo "No bitbucket auth token set, skipping upload"
    fi

    if [ -n "$DOCKER_USER" ] && [ -n "$DOCKER_PASSWORD" ]; then

        # promoting alpha to version defined by tag name

        echo "Tagging docker image with $BITBUCKET_TAG"
        docker pull "$HASHED_IMAGE"
        TAGGED_IMAGE="amdatu/${APPNAME}:${BITBUCKET_TAG}"
        docker tag "$HASHED_IMAGE" "$TAGGED_IMAGE"
        echo "Pushing docker image"
        docker login --username="$DOCKER_USER" --password="$DOCKER_PASSWORD"
        docker push "$TAGGED_IMAGE"

        if [ "$BITBUCKET_TAG" == "production" ]; then
            echo "Also tagging and pushing latest"
            LATEST_IMAGE="amdatu/${APPNAME}:latest"
            docker tag "$HASHED_IMAGE" "$LATEST_IMAGE"
            docker push "$LATEST_IMAGE"
        fi
    else
        echo "No docker user/password set, skipping image tag and push"
    fi

else
    echo "Not on master branch, and not triggered by tag: skipping publishing"
fi

echo "Done"
