#!/usr/bin/env bash
curl -XPOST http://localhost:8000/deployment -d '{
  "deploymentType": "blue-green",
  "namespace": "default",
  "email": "admin@amdatu.org",
  "password": "test",
  "useHealthCheck": true,
  "newVersion": "r3",
  "appName": "nginx",
  "replicas": 2,
  "frontend": "aws-rti-todo-development.amdatu.com",
  "podspec": {
    "containers": [{
        "image": "nginx",
        "name": "nginx",
        "ports": [{
          "containerPort": 80
        }]
        }
    ]
  }
}'