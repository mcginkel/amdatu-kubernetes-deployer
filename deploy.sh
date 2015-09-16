#!/usr/bin/env bash
curl -XPOST http://localhost:8000/deployment -d '{
  "deploymentType": "blue-green",
  "namespace": "default",
  "useHealthCheck": false,
  "newVersion": "r-4",
  "appName": "nginx",
  "replicas": 2,
  "vulcanFrontend": "rti-todo.amdatu.com",
  "podspec": {
    "containers": [{
        "image": "nginx",
        "name": "nginx",
        "ports": [{
          "containerPort": 8080
        }]
        }
    ]
  }
}'