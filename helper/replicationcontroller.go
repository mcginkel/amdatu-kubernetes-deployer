/*
Copyright (c) 2016 The Amdatu Foundation

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package helper

import (
	"time"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-go/api/v1"
	k8sClient "bitbucket.org/amdatulabs/amdatu-kubernetes-go/client"
)

func ShutdownReplicationController(rc *v1.ReplicationController, k8sclient *k8sClient.Client, logger logger.Logger) error {
	logger.Printf("Scaling down replication controller: %v\n", rc.Name)

	err := k8sclient.Patch(rc.Namespace, "replicationcontrollers", rc.Name, `{"spec": {"replicas": 0}}`)
	if err != nil {
		logger.Printf("Error scaling down replication controller: %v\n", err.Error())
	}

	successChan := make(chan bool)

	go waitForScaleDown(rc, k8sclient, successChan)

	select {
	case <-successChan:
		logger.Println("Scaledown successful")
	case <-time.After(time.Second * 90):
		logger.Println("Scaledown failed")
		successChan <- false
	}

	return k8sclient.DeleteReplicationController(rc.Namespace, rc.Name)
}

func waitForScaleDown(rc *v1.ReplicationController, k8sclient *k8sClient.Client, successChan chan bool) {
	for {
		select {
		case <-successChan:
			return
		default:

			labels := map[string]string{"app": rc.Labels["app"], "version": rc.Labels["version"]}
			pods, _ := k8sclient.ListPodsWithLabel(rc.Namespace, labels)

			if CountRunningPods(pods.Items) > 0 {
				time.Sleep(1 * time.Second)
			} else {
				successChan <- true
				return
			}
		}
	}
}
