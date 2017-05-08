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
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"strings"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/cluster"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/helper"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/proxies"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/pkg/api/v1"
)

type bluegreen struct {
	deployer *cluster.Deployer
}

func NewBlueGreen(deployer *cluster.Deployer) *bluegreen {
	return &bluegreen{deployer}
}

func (bluegreen *bluegreen) Deploy() error {

	descriptor := bluegreen.deployer.Deployment.Descriptor

	bluegreen.deployer.Logger.Println("Starting blue-green deployment")

	backendId := descriptor.Namespace + "-" + bluegreen.deployer.CreateRcName()
	bluegreen.deployer.Logger.Printf("Prepare proxy backend %v....\n", backendId)
	if descriptor.Frontend != "" {
		frontend := proxies.Frontend{
			Type:              "http",
			Hostname:          descriptor.Frontend,
			BackendId:         backendId,
			RedirectWwwPrefix: descriptor.RedirectWww,
		}

		if _, err := bluegreen.deployer.Config.ProxyConfigurator.CreateFrontEnd(&frontend); err != nil {
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

	if err := bluegreen.createReplicationController(); err != nil {
		bluegreen.deployer.Logger.Println(err)
		return err
	}

	if len(service.Spec.Ports) > 0 {
		port := selectPort(service.Spec.Ports)
		bluegreen.deployer.Logger.Printf("Adding backend for port %v\n", port)
		bluegreen.deployer.Config.ProxyConfigurator.AddBackendServer(backendId, service.Spec.ClusterIP, int32(port.Port),
			descriptor.UseCompression, descriptor.AdditionHttpHeaders)
	}

	if descriptor.Frontend != "" {
		if err := bluegreen.deployer.Config.ProxyConfigurator.WaitForBackend(backendId, bluegreen.deployer.Logger); err != nil {
			bluegreen.deployer.Logger.Println(err)
			return err
		}

		bluegreen.deployer.Logger.Println("Switch proxy backends....")

		if err := bluegreen.deployer.Config.ProxyConfigurator.SwitchBackend(descriptor.Frontend, backendId); err != nil {
			bluegreen.deployer.Logger.Printf("%v", err)
			return err
		}
	}

	_, err = bluegreen.deployer.CreateOrUpdatePersistentService()
	if err != nil {
		bluegreen.deployer.Logger.Println(err)
		return err
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

	descriptor := bluegreen.deployer.Deployment.Descriptor

	_, err := bluegreen.deployer.CreateReplicationController()
	if err != nil {
		return err
	}

	if descriptor.Replicas == 0 {
		return nil
	}

	if descriptor.UseHealthCheck && !descriptor.IgnoreHealthCheck {
		return bluegreen.waitForPods(bluegreen.deployer.CreateRcName(), bluegreen.deployer.Deployment.Version)
	} else {
		return nil
	}
}

func (bluegreen *bluegreen) waitForPods(name, version string) error {
	healthChan := make(chan bool, 1)

	bluegreen.deployer.Logger.Printf("Waiting up to %v seconds for pods to start and to become healthy\n", bluegreen.deployer.Config.HealthTimeout)

	go bluegreen.checkPods(name, version, healthChan)

	select {
	case healthy := <-healthChan:
		if healthy {
			return nil
		} else {
			return errors.New("Error while waiting for pods to become healthy")
		}
	case <-time.After(time.Duration(bluegreen.deployer.Config.HealthTimeout) * time.Second):
		healthChan <- false
		return errors.New("Timeout waiting for pods to become healthy")
	}

}

func (bluegreen *bluegreen) checkPods(name, version string, healthChan chan bool) {

	descriptor := bluegreen.deployer.Deployment.Descriptor

	for {
		select {
		case <-healthChan:
			return
		default:
			{
				selector := map[string]string{"name": name, "version": bluegreen.deployer.Deployment.Version}
				pods, listErr := bluegreen.deployer.K8client.
					Pods(descriptor.Namespace).
					List(meta.ListOptions{
						LabelSelector: labels.SelectorFromSet(selector).String(),
					})
				if listErr != nil {
					bluegreen.deployer.Logger.Printf(fmt.Sprintf("Error listing pods for new deployment: %v\n", listErr))
					healthChan <- false

					return
				}

				nrOfPods := helper.CountRunningPods(pods.Items)

				if nrOfPods == descriptor.Replicas {
					healthy := true

					for _, pod := range pods.Items {
						if !bluegreen.checkPodHealth(&pod) {
							healthy = false
							break
						}
					}

					if healthy {
						healthChan <- true
						bluegreen.deployer.Logger.Println("Deployment healthy!")
						return
					} else {
						bluegreen.deployer.Logger.Println("Deployment not healthy yet, retrying in 1 second")
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

	descriptor := bluegreen.deployer.Deployment.Descriptor

	var resp *http.Response
	var err error

	port := cluster.FindHealthcheckPort(pod)
	url := bluegreen.deployer.GetHealthcheckUrl(pod.Status.PodIP, port)

	//bluegreen.deployer.Logger.Printf("Checking pod health with healthcheck type %s on url %s",
	//	descriptor.HealthCheckType, url);

	if strings.EqualFold(descriptor.HealthCheckType, "simple") {
		resp, err = http.Get(url)
		if err != nil {
			bluegreen.logHealth(pod, "{\"simplehealthcheck\": \"http get failed\"}")
			return false
		}
		if resp.StatusCode != 200 {
			bluegreen.logHealth(pod, "{\"simplehealthcheck\": \"http get statuscode != 200\"}")
			return false
		}
		bluegreen.logHealth(pod, "{\"simplehealthcheck\": \"http get success\"}")
		return true
	} else {
		// default to healthcheck type "probe"
		resp, err = http.Post(url, "application/json", nil)
		if err != nil {
			bluegreen.logHealth(pod, "{\"probehealthcheck\": \"failed: "+err.Error()+"\"}")
			return false
		}
		defer resp.Body.Close()

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			bluegreen.logHealth(pod, "{\"probehealthcheck\": \"failed: "+err.Error()+"\"}")
			return false
		}

		bluegreen.logHealth(pod, string(body))

		var dat = HealthCheckEvent{}
		if err := json.Unmarshal(body, &dat); err != nil {
			bluegreen.deployer.Logger.Println("Error parsing healthcheck: " + err.Error())
			return false
		}

		return dat.Healthy
	}
}

func (bluegreen *bluegreen) logHealth(pod *v1.Pod, health string) {
	descriptor := bluegreen.deployer.Deployment.Descriptor
	bluegreen.deployer.Config.EtcdRegistry.StoreHealth(descriptor.Namespace, bluegreen.deployer.Deployment.Id, pod.Name, health)
}

type HealthCheckEvent struct {
	Healthy bool `json:"healthy,omitempty"`
}
