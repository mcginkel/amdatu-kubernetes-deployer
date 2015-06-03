#!/usr/bin/env bash
curl -XPOST http://10.100.103.10:8000/deployment -d '{
"deploymentType": "rolling",
"useHealthCheck": true,
"newVersion": "release-58",
"appName": "todo",
"replicas": 2,
"vulcanFrontend": "rti-todo.amdatu.com",
"podspec": {
    "containers": [
        { "image":"rti-docker-registry.amdatu.com:5000/amdatu/todo:prod-release-58",
          "name":"todo", 
          "ports": [
              {
                "containerPort": 8080
              }
            ]}
        ]}
}'