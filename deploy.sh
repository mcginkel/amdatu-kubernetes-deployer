#!/usr/bin/env bash
curl -XPOST http://localhost:8000/deployment -d '{
"deploymentType": "rolling",
"useHealthCheck": false,
"newVersion": "release-57",
"appName": "todo",
"replicas": 2,
"vulcanFrontend": "rti-todo.amdatu.com",
"podspec": {
    "containers": [
        { "image":"rti-docker-registry.amdatu.com:5000/amdatu/todo:prod-release-57",
          "name":"todo", 
          "ports": [
              {
                "containerPort": 8080
              }
            ]}
        ]}
}'