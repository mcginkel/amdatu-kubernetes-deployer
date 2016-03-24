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
package redeploy

import (
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/cluster"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-go/api/v1"
	"time"
)

/**
Deployer to re-deploy an existing version.
Essentially re-deploying only means restarting the currently running pods. All other configuration, like the load balancer and service, should already be available.

1) In a rolling fashion, kill all existing pods one at a time.
2) Kubernetes will take care of rescheduling the replicas
*/

type redeployer struct {
	deployer *cluster.Deployer
}

func NewRedeployer(deployer *cluster.Deployer) *redeployer {
	return &redeployer{deployer}
}

func (redeployer *redeployer) Deploy() error {
	redeployer.deployer.Logger.Println("Redeploying")

	pods, error := redeployer.deployer.FindCurrentPods(true)

	if error != nil {
		redeployer.deployer.Logger.Println(error)
		return error
	}

	podNames := make(map[string]*v1.Pod, len(pods))
	for _, p := range pods {
		podNames[p.Name] = &p
	}

	for _, pod := range pods {

		callBack := make(chan bool)
		go redeployer.waitForNewPod(callBack, podNames)

		redeployer.deployer.Logger.Printf("Deleting pod %v\n", pod.ObjectMeta.Name)
		redeployer.deployer.DeletePod(pod)

		<-callBack

		close(callBack)
	}

	return nil
}

func (redeployer *redeployer) waitForNewPod(callback chan bool, existingPods map[string]*v1.Pod) {
	podSelector := map[string]string{"name": redeployer.deployer.CreateRcName(), "version": redeployer.deployer.Deployment.NewVersion}

	watchNew, signals, err := redeployer.deployer.K8client.WatchPodsWithLabel(redeployer.deployer.Deployment.Namespace, podSelector)

	if err != nil {
		redeployer.deployer.Logger.Println(err)
		signals <- "cancel"
		callback <- false
		return
	}

	timeout := make(chan string)
	go func() {
		time.Sleep(time.Minute * 5)
		timeout <- "TIMEOUT"
		close(timeout)
		signals <- "cancel"
		callback <- false
	}()

	for evnt := range watchNew {
		podObj := evnt.Object

		if evnt.Type == "MODIFIED" && existingPods[podObj.Name] == nil && podObj.Status.PodIP != "" {
			redeployer.deployer.CheckPodHealth(&podObj)
			redeployer.deployer.Logger.Println("Found new pod")
			callback <- true
			signals <- "cancel"
			break
		}
	}
}
