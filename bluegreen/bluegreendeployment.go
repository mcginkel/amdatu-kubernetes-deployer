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
package bluegreen

/**
1) Create new RC, with a label name that includes the version.
2) Watch Kubernetes for Pods to become RUNNING
3) Switch backend in proxy
4) Remove old backend from proxy
5) Remove old RC from Kubernetes
*/

import (
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/cluster"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/proxies"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-go/api/v1"
	"errors"
	"time"
)

type bluegreen struct {
	deployer *cluster.Deployer
}

func NewBlueGreen(deployer *cluster.Deployer) *bluegreen {
	return &bluegreen{deployer}
}

func (bluegreen *bluegreen) Deploy() error {

	bluegreen.deployer.Logger.Println("Starting blue-green deployment")

	backendId := bluegreen.deployer.Deployment.Namespace + "-" + bluegreen.deployer.CreateRcName()
	bluegreen.deployer.Logger.Printf("Prepare proxy backend %v....\n", backendId)
	if bluegreen.deployer.Deployment.Frontend != "" {
		frontend := proxies.Frontend{
			Type:      "http",
			Hostname:  bluegreen.deployer.Deployment.Frontend,
			BackendId: backendId,
		}

		if _, err := bluegreen.deployer.ProxyConfigurator.CreateFrontEnd(&frontend); err != nil {
			return err
		}
	} else {
		bluegreen.deployer.Logger.Println("No frontend configured in deployment, skipping creation")
	}

	service, err := bluegreen.deployer.CreateService()
	if err != nil {
		bluegreen.deployer.Logger.Println(err)
		return err
	}

	_, err = bluegreen.deployer.CreatePersistentService()
	if err != nil {
		bluegreen.deployer.Logger.Println(err)
		return err
	}

	if err := bluegreen.createReplicationController(); err != nil {
		bluegreen.deployer.Logger.Println(err)
		return err
	}

	if len(service.Spec.Ports) > 0 {
		port := selectPort(service.Spec.Ports)
		bluegreen.deployer.Logger.Printf("Adding backend for port %v\n", port)
		bluegreen.deployer.ProxyConfigurator.AddBackendServer(backendId, service.Spec.ClusterIP, int32(port.Port),
			bluegreen.deployer.Deployment.UseCompression)
	}

	if bluegreen.deployer.Deployment.Frontend != "" {
		bluegreen.deployer.Logger.Println("Sleeping for 20 seconds for proxy to reload...")

		time.Sleep(time.Second * 20)

		bluegreen.deployer.Logger.Println("Switch proxy backends....")

		if err := bluegreen.deployer.ProxyConfigurator.SwitchBackend(bluegreen.deployer.Deployment.Frontend, backendId); err != nil {
			bluegreen.deployer.Logger.Printf("%v", err)
			return err
		}
	}

	bluegreen.deployer.Logger.Println("Cleaning up old deployments....")
	bluegreen.deployer.CleaupOldDeployments()

	return nil
}

func selectPort(ports []v1.ServicePort) v1.ServicePort {
	if len(ports) > 1 {
		for _, port := range ports {
			if port.Name != "healthcheck" {
				return port
			}
		}
	}

	return ports[0]
}

func (bluegreen *bluegreen) createReplicationController() error {

	_, err := bluegreen.deployer.CreateReplicationController()
	if err != nil {
		return err
	}

	if bluegreen.deployer.Deployment.Replicas == 0 {
		return nil
	}

	callBack := make(chan string)
	timeout := make(chan string)

	go bluegreen.watchPods(bluegreen.deployer.CreateRcName(), bluegreen.deployer.Deployment.NewVersion, callBack)
	go func() {
		time.Sleep(1 * time.Minute)
		timeout <- "TIMEOUT"
	}()

	//Wait for either the pods to report healthy, or the timeout to happen
	select {
	case msg := <-callBack:
		if msg == "ERROR" {
			return errors.New("Did not find enough running pods")
		}
	case <-timeout:
		return errors.New("Timeout waiting for pods")
	}

	return nil
}

func (bluegreen *bluegreen) watchPods(name, version string, callback chan string) error {
	podSelector := map[string]string{"name": name, "version": bluegreen.deployer.Deployment.NewVersion}

	watchNew, signals, err := bluegreen.deployer.K8client.WatchPodsWithLabel(bluegreen.deployer.Deployment.Namespace, podSelector)

	if err != nil {
		bluegreen.deployer.Logger.Println(err)
		callback <- "ERROR"
		return err
	}

	bluegreen.deployer.Logger.Println("Waiting for pods to spin up...")

	for pod := range watchNew {
		podObj := pod.Object

		if podObj.Status.Phase == "Running" {

			if err := bluegreen.deployer.CheckPodHealth(&podObj); err != nil {
				signals <- "cancel"
				callback <- "ERROR"

				return err
			}

			pods, listErr := bluegreen.deployer.K8client.ListPodsWithLabel(bluegreen.deployer.Deployment.Namespace, podSelector)
			if listErr != nil {
				bluegreen.deployer.Logger.Println(listErr)
				signals <- "cancel"
				callback <- "ERROR"

				return err
			}

			nrOfPods := bluegreen.deployer.CountRunningPods(pods.Items)
			if nrOfPods == bluegreen.deployer.Deployment.Replicas {
				bluegreen.deployer.Logger.Printf("Found enough running pods (%v), continue to switch versions...\n", nrOfPods)
				callback <- "FINISHED"
				signals <- "cancel"
				return nil
			} else {
				bluegreen.deployer.Logger.Printf("Waiting for %v more pods...\n", bluegreen.deployer.Deployment.Replicas-nrOfPods)
			}
		}
	}

	return nil
}
