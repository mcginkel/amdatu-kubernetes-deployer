#!/usr/bin/env bash
curl -o output.txt  -XPOST http://10.150.16.65:8000/deployment -d '{
  "deploymentType": "blue-green",
  "namespace": "default",
  "useHealthCheck": false,
  "newVersion": "#",
  "appName": "deploymentctl",
  "replicas": 1,
  "frontend": "aws-rti-deployer-development.amdatu.com",
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
        "env": [
            {
                "name": "deployer_host",
                "value": "10.150.16.65"
            },
            {
                "name": "deployer_port",
                "value": "8000"
            },
            {

                "name": "kubernetes_url",
                "value": "http://10.150.16.32:8080"
            },
            {
                "name": "redis_host",
                "value": "10.201.159.165"
            },
            {
                "name": "port",
                "value": "8585"
            },
            {
                "name": "dashboard_url",
                "value": "https://aws-rti-dashboard-development.amdatu.com"
            }
        ],
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