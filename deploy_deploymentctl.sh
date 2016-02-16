#!/usr/bin/env bash
curl -o output.txt  -XPOST http://localhost:8000/deployment -d '{
  "deploymentType": "blue-green",
  "namespace": "default",
  "useHealthCheck": false,
  "newVersion": "#",
  "appName": "deploymentctl",
  "replicas": 1,
  "frontend": "cloud-rti-demo.amdatu.com",
  "podspec": {
  "imagePullSecrets": [{
            "name": "amdatu"
        }],

    "containers": [
    {
        "image": "amdatu/deploymentctl-backend:alpha",
        "name" : "backend",
        "ports": [{
          "containerPort": 8585
        }],
        "volumeMounts": [{
            "mountPath": "/app/webroot",
            "name": "www",
            "readOnly": true
        }],
        "imagePullSecrets" : [
            {
                "name": "amdatu"
            }
        ],
        "imagePullPolicy": "Always"
    },

    {
        "imagePullPolicy": "Always",
        "image": "amdatu/deployerctl-frontend:alpha",
        "name" : "frontend",
        "volumeMounts": [{
            "mountPath": "/data",
            "name": "www"
      }],
      "imagePullSecrets" : [
            {
                "name": "amdatu"
            }
        ],
      "imagePullPolicy": "Always",
      "lifecycle": {
        "postStart": {
            "exec": {
                "command": ["cp", "-R", "/www/dist/.", "/data"]
            }
        }
      }
    }],

    "volumes": [{
      "name": "www",
      "emptyDir": {}
    }]
  }
}'

cat output.txt

FAILED=$(cat output.txt | grep "Deployment Failed" | wc -l)
if [ $FAILED -ne 0 ]; then
    exit 1
fi

exit 0