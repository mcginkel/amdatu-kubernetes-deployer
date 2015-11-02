#!/usr/bin/env bash
curl -XPOST http://localhost:8000/deployment -d '{
  "deploymentType": "blue-green",
  "namespace": "default",
  "useHealthCheck": false,
  "newVersion": "release-18",
  "appName": "todo",
  "replicas": 2,
  "frontend": "aws-rti-todo-development.amdatu.com",
  "podspec": {
    "imagePullSecrets": [{
            "name": "amdatu"
        }],
    "containers": [{
        "image": "amdatu/todo-demonstrator:alpha",
        "imagePullPolicy": "Always",
        "name": "todo",
        "ports": [{
          "containerPort": 8080
        }],

        "env": [
          {
          "name": "version",
          "value": "release-18"
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
          "value": "10.150.16.64:9092"
          },
          {
          "name": "hostname",
          "value": "rti-todo.amdatu.com"
          }
        ]
      }
    ]
  }
}'