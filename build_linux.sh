#!/bin/bash
CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o deployer .
#docker login -e dockerhub@amdatu.com -p WDgGd6MzfFpBFf9j -u amdatuci
docker build --tag amdatu/kubernetes-deployer:alpha .
docker push amdatu/kubernetes-deployer:alpha