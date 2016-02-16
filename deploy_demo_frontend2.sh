#!/usr/bin/env bash
curl -o output.txt  -XPOST http://localhost:8000/deployment -d '{
  "deploymentType": "blue-green",
  "namespace": "default",
  "useHealthCheck": false,
  "newVersion": "1",
  "appName": "cloudrti-fr",
  "replicas": 1,
  "frontend": "cloud-rti-demo.amdatu.com",
  "podspec": {
    "containers": [
    {
        "image": "nginx",
        "name" : "nginx",
        "ports": [{
          "containerPort": 80
        }],
        "volumeMounts": [{
            "mountPath": "/usr/share/nginx/html",
            "name": "www",
            "readOnly": true
        }]
    },

    {
        "imagePullPolicy": "Always",
        "image": "amdatu/cloudrti-demo-frontend",
        "name" : "cloudrti-demo-frontend",
        "volumeMounts": [{
            "mountPath": "/data",
            "name": "www"
      }],
      "lifecycle": {
        "postStart": {
            "exec": {
                "command": ["cp", "-R", "/www/.", "/data"]
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