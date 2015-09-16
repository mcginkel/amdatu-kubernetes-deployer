#!/bin/bash
CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o main .
#docker login -e dockerhub@amdatu.com -p WDgGd6MzfFpBFf9j -u amdatuci
docker build --tag amdatu/kubernetes-deployer .
docker push amdatu/kubernetes-deployer