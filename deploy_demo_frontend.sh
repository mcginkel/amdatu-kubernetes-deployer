#!/usr/bin/env bash
curl -o output.txt  -XPOST http://10.150.16.64:8000/deployment -d '{
  "deploymentType": "blue-green",
  "namespace": "default",
  "useHealthCheck": false,
  "newVersion": "20",
  "appName": "cloudrti-demo-fr",
  "replicas": 1,
  "frontend": "cloud-rti-demo.amdatu.com",
  "podspec": {
    "imagePullSecrets": [{
            "name": "amdatu"
        }],
    "containers": [
    {
        "image": "amdatu/cloudrti-demo-frontend",
        "name" : "cloudrti-demo-frontend",
        "ports": [{
          "containerPort": 80
        }]
    }]
  }
}'

cat output.txt

FAILED=$(cat output.txt | grep "Deployment Failed" | wc -l)
if [ $FAILED -ne 0 ]; then
    exit 1
fi

exit 0