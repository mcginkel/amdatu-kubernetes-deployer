#!/usr/bin/env bash
curl -o output.txt  -XPOST http://localhost:8000/deployment -d '{
  "deploymentType": "blue-green",
  "namespace": "default",
  "useHealthCheck": true,
  "newVersion": "release-188",
  "appName": "todo",
  "replicas": 3,
  "frontend": "aws-rti-todo-development.amdatu.com",
  "podspec": {
    "imagePullSecrets": [{
            "name": "amdatu"
        }],
    "containers": [{
        "image": "amdatu/todo-demonstrator:leaky",
        "resources": {
            "limits": {
                "memory": "1500Mi",
                "cpu": "500m"
            }
        },
        "imagePullPolicy": "Always",
        "name": "todo",
        "ports": [{
          "containerPort": 8080
        }],
        "env": [
          {
          "name": "version",
          "value": "release-188"
          },
          {
          "name": "mongo",
          "value": "10.150.16.64"
          },
          {
          "name": "dbname",
          "value": "todo-app"
          },
          {
          "name": "kafka",
          "value": "10.151.16.64:9092"
          },
          {
          "name": "hostname",
          "value": "aws-rti-todo-development.amdatu.com"
          }
        ]
      }
    ]
  }
}'

cat output.txt

FAILED=$(cat output.txt | grep "Deployment Failed" | wc -l)
if [ $FAILED -ne 0 ]; then
    exit 1
fi

exit 0