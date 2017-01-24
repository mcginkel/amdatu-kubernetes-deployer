#!/bin/bash

# this should be called within the golang docker container during build OR from the bitbucket pipeline

# fail immedialtely
set -e
set -o pipefail

# version number of glibc and go
GLIBC_VERSION=2.23-r3
GOLANG_VERSION=1.7.4

# save where we are coming from, that's where our sources are
SRCDIR=`pwd`

# install glibc, needed by go
apk --no-cache add curl wget ca-certificates
wget -q -O /etc/apk/keys/sgerrand.rsa.pub https://raw.githubusercontent.com/sgerrand/alpine-pkg-glibc/master/sgerrand.rsa.pub
wget https://github.com/sgerrand/alpine-pkg-glibc/releases/download/${GLIBC_VERSION}/glibc-${GLIBC_VERSION}.apk
apk add glibc-2.23-r3.apk

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

# build and install binary
cd "${PACKAGE_PATH}"
go install -v

# remove sources and tmp files
cd "${GOPATH}"
rm -r src
rm -r pkg

# remove go, it's huge and not needed anymore
rm -rf /usr/local/go

# remove apk cache
rm -rf /var/cache/apk/*
