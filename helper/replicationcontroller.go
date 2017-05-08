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
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"time"
)

func ShutdownReplicationController(rc *v1.ReplicationController, k8sclient *kubernetes.Clientset, logger logger.Logger) error {
	logger.Printf("Scaling down replication controller: %v\n", rc.Name)

	replicas := int32(0)
	rc.Spec.Replicas = &replicas
	_, err := k8sclient.ReplicationControllers(rc.Namespace).Update(rc)
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

	return k8sclient.ReplicationControllers(rc.Namespace).Delete(rc.Name, &meta.DeleteOptions{})
}

func waitForScaleDown(rc *v1.ReplicationController, k8sclient *kubernetes.Clientset, successChan chan bool) {
	for {
		select {
		case <-successChan:
			return
		default:

			selector := map[string]string{"app": rc.Labels["app"], "version": rc.Labels["version"]}
			pods, _ := k8sclient.
				Pods(rc.Namespace).
				List(meta.ListOptions{
					LabelSelector: labels.SelectorFromSet(selector).String(),
				})

			if CountRunningPods(pods.Items) > 0 {
				time.Sleep(1 * time.Second)
			} else {
				successChan <- true
				return
			}
		}
	}
}
