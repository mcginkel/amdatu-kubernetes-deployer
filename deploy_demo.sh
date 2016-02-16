#!/usr/bin/env bash
curl -o output.txt  -XPOST http://localhost:8000/deployment -d '{
  "deploymentType": "blue-green",
  "namespace": "default",
  "useHealthCheck": false,
  "newVersion": "#",
  "appName": "cloudrti-demo",
  "replicas": 2,
  "frontend": "cloud-rti-demo.amdatu.com",
  "podspec": {
    "imagePullSecrets": [{
            "name": "amdatu"
        }],
    "containers": [{
        "image": "amdatu/cloudrti-demo:alpha",
        "imagePullSecrets" : [
            {
                "name": "amdatu"
            }
        ],
        "imagePullPolicy": "Always",
        "name": "cloudrti-demo",
        "ports": [{
          "name": "http",
          "containerPort": 8080
        },
        {
            "name": "healthcheck",
            "containerPort": 9999
        }],
        "readinessProbe": {
            "httpGet": {
                "path": "/health",
                "port": 9999
            },
            "initialDelaySeconds": 15,
            "timeoutSeconds": 1
        }

      }
    ],
    "terminationGracePeriodSeconds": 0
  }
}'

cat output.txt

FAILED=$(cat output.txt | grep "Deployment Failed" | wc -l)
if [ $FAILED -ne 0 ]; then
    exit 1
fi

exit 0