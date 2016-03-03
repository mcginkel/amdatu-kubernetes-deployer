#!/usr/bin/env bash
curl -o output.txt  -XPOST http://10.150.16.64:8000/deployment -d '{
  "deploymentType": "blue-green",
  "namespace": "workouttraxx",
  "useHealthCheck": false,
  "newVersion": "15",
  "appName": "workouttraxx-website",
  "replicas": 1,
  "frontend": "cloud-rti-demo.amdatu.com",
  "podspec": {
    "containers": [{
      "image": "nginx",
      "name": "website",
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
      "image": "paulbakker/git-sync",
      "name": "git-sync",
      "imagePullPolicy" : "Always",
      "env": [
        {
            "name": "GIT_SYNC_REPO",
            "value": "https://github.com/workouttraxx/workouttraxx.github.io.git"
        },
        {
            "name": "GIT_SYNC_WAIT",
            "value": "10"
        }
      ],
      "volumeMounts": [{
        "mountPath": "/git",
        "name": "www"
      }]
    }
    ],
    "volumes": [{
      "name": "www",
      "emptyDir": {}
    }]
  }
}
'

cat output.txt

FAILED=$(cat output.txt | grep "Deployment Failed" | wc -l)
if [ $FAILED -ne 0 ]; then
    exit 1
fi

exit 0