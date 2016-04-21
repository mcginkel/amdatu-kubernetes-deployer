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
	"fmt"
	"net/http"
	"io/ioutil"
	"encoding/json"
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
		bluegreen.deployer.ProxyConfigurator.WaitForBackend(backendId)

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

	if bluegreen.deployer.Deployment.UseHealthCheck {
		return bluegreen.waitForPods(bluegreen.deployer.CreateRcName(), bluegreen.deployer.Deployment.NewVersion)
	} else {
		return nil
	}
}

func (bluegreen *bluegreen) waitForPods(name, version string) error {
	healthChan := make(chan bool)

	bluegreen.deployer.Logger.Printf("Waiting %v seconds for pods to start and to become healthy\n", bluegreen.deployer.HealthcheckTimeout)

	go bluegreen.checkPods(name, version, healthChan)

	select {
	case healthy := <- healthChan:
		if healthy {
			return nil
		} else {
			return errors.New("Error while waiting for pods to become healthy")
		}
	case <-time.After(time.Duration(bluegreen.deployer.HealthcheckTimeout) * time.Second):
		healthChan <- false
		return errors.New("Timeout waiting for pods to become healthy")
	}

}

func (bluegreen *bluegreen) checkPods(name, version string, healthChan chan bool) {
	for {
		select {
		case <-healthChan:
			return
		default:
			{
				podSelector := map[string]string{"name": name, "version": bluegreen.deployer.Deployment.NewVersion}
				pods, listErr := bluegreen.deployer.K8client.ListPodsWithLabel(bluegreen.deployer.Deployment.Namespace, podSelector)
				if listErr != nil {
					bluegreen.deployer.Logger.Printf(fmt.Sprintf("Error listing pods for new deployment: %\n", listErr))
					healthChan <- false

					return
				}

				nrOfPods := bluegreen.deployer.CountRunningPods(pods.Items)

				if nrOfPods == bluegreen.deployer.Deployment.Replicas {
					healthy := true

					for _, pod := range pods.Items {
						if !bluegreen.checkPodHealth(&pod) {
							healthy = false
							break
						}
					}

					if healthy {
						healthChan <- true
						return
					} else {
						time.Sleep(1 * time.Second)
					}

				} else {
					time.Sleep(1 * time.Second)
				}
			}

		}
	}
}

func (bluegreen *bluegreen) checkPodHealth(pod *v1.Pod) bool {
	var resp *http.Response
	var err error


	port := cluster.FindHealthcheckPort(pod)

	url := bluegreen.deployer.GetHealthcheckUrl(pod.Status.PodIP, port)

	resp, err = http.Post(url, "application/json", nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return false
	}

	var dat = HealthCheckEvent{}
	if err := json.Unmarshal(body, &dat); err != nil {
		return false
	}

	return dat.Healthy
}

type HealthCheckEvent struct {
	Healthy bool `json:"healthy,omitempty"`
}
