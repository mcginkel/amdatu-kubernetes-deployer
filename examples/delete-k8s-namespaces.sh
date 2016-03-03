#!/usr/bin/env bash

# Run this script on a Kubernetes master

for each in $(kubectl get ns -o jsonpath="{.items[*].metadata.name}");
do
  echo $each
  if [ $each != "default" ]; then
        kubectl delete ns $each
  fi
done