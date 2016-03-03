#!/usr/bin/env bash
curl -o output.txt  -XPOST http://localhost:8000/deployment -d '{
  "deploymentType": "blue-green",
  "namespace": "default",
  "useHealthCheck": false,
  "newVersion": "#",
  "appName": "todo",
  "replicas": 1,
  "frontend": "aws-rti-todo-development.amdatu.com",
  "podspec": {
    "imagePullSecrets": [{
            "name": "amdatu"
        }],
    "containers": [{
        "image": "amdatu/todo-demonstrator:alpha",
        "imagePullSecrets" : [
            {
                "name": "amdatu"
            }
        ],
        "imagePullPolicy": "Always",
        "name": "todo",
        "ports": [{
          "containerPort": 8080
        }],
        "env": [
          {
          "name": "version",
          "value": "Alpha"
          },
          {
          "name": "mongo",
          "value": "10.150.16.90"
          },
          {
          "name": "dbname",
          "value": "todo-app"
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