#!/bin/bash

# this should be called within an alpine docker container during manual build OR from the bitbucket pipeline

# fail immedialtely
set -e
set -o pipefail

# version number of glibc and go
GLIBC_VERSION=2.23-r3
GOLANG_VERSION=1.7.4

# save where we are coming from, that's where our sources are
SRCDIR=`pwd`

# install some common needed packages
apk --no-cache add curl wget ca-certificates

# install glibc, needed by go
wget -q -O /etc/apk/keys/sgerrand.rsa.pub https://raw.githubusercontent.com/sgerrand/alpine-pkg-glibc/master/sgerrand.rsa.pub
wget -q -O /tmp/glibc.apk https://github.com/sgerrand/alpine-pkg-glibc/releases/download/${GLIBC_VERSION}/glibc-${GLIBC_VERSION}.apk
apk add /tmp/glibc.apk
rm  /tmp/glibc.apk

# install golang
GOLANG_URL=https://storage.googleapis.com/golang/go$GOLANG_VERSION.linux-amd64.tar.gz
wget -q "${GOLANG_URL}" -O /tmp/golang.tar.gz
mkdir -p /usr/local
tar -C /usr/local -xzf /tmp/golang.tar.gz
rm /tmp/golang.tar.gz

# configure go basics
export GOPATH="/go"
mkdir -p "$GOPATH/src" "$GOPATH/bin" && chmod -R 777 "$GOPATH/bin"
export PATH="$GOPATH/bin:/usr/local/go/bin:$PATH"

# create correct golang dir structure for this project
PACKAGE_PATH="${GOPATH}/src/bitbucket.org/amdatulabs/amdatu-kubernetes-deployer"
mkdir -p "${PACKAGE_PATH}"

# copy sources to new directory
cd "${SRCDIR}"
tar -cO --exclude .git . | tar -xv -C "${PACKAGE_PATH}"

# build and install binary for running in the resulting docker image
cd "${PACKAGE_PATH}"
go install -v

if [ -n "$BB_AUTH_STRING" ] && [ "$BITBUCKET_BRANCH"=="master" ]; then

    VERSION="alpha"

    if [ -n "$BITBUCKET_TAG" ]; then
        VERSION=$BITBUCKET_TAG
    fi

    BASENAME="amdatu-kubernetes-deployer"
    LINUX_NAME="$BASENAME-linux_amd64-$VERSION"
    MACOS_NAME="$BASENAME-macos_amd64-$VERSION"
    WIN_NAME="$BASENAME-windows_amd64-$VERSION.exe"

    # build binaries for export
    CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o "$LINUX_NAME" .
    CGO_ENABLED=0 GOOS=darwin go build -a -installsuffix cgo -o "$MACOS_NAME" .
    CGO_ENABLED=0 GOOS=windows go build -a -installsuffix cgo -o "$WIN_NAME" .

    # post binaries to download section of bitbucket

    curl -X POST "https://${BB_AUTH_STRING}@api.bitbucket.org/2.0/repositories/${BITBUCKET_REPO_OWNER}/${BITBUCKET_REPO_SLUG}/downloads" --form files=@"$LINUX_NAME"
    curl -X POST "https://${BB_AUTH_STRING}@api.bitbucket.org/2.0/repositories/${BITBUCKET_REPO_OWNER}/${BITBUCKET_REPO_SLUG}/downloads" --form files=@"$MACOS_NAME"
    curl -X POST "https://${BB_AUTH_STRING}@api.bitbucket.org/2.0/repositories/${BITBUCKET_REPO_OWNER}/${BITBUCKET_REPO_SLUG}/downloads" --form files=@"$WIN_NAME"

fi

# remove sources and tmp files
cd "${GOPATH}"
rm -r src
rm -r pkg

# remove go, it's huge and not needed anymore
rm -rf /usr/local/go

# remove apk cache
rm -rf /var/cache/apk/*
