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

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"time"

	"strings"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/cluster"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/k8s"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	"k8s.io/client-go/pkg/api/v1"
)

type bluegreen struct {
	clusterManager *cluster.ClusterManager
}

func NewBlueGreen(clusterManager *cluster.ClusterManager) *bluegreen {
	return &bluegreen{clusterManager}
}

func (bluegreen *bluegreen) Deploy() error {

	deployment := bluegreen.clusterManager.Deployment
	descriptor := deployment.Descriptor
	logger := bluegreen.clusterManager.Logger

	logger.Println("Starting blue-green deployment")

	logger.Println("Creating Replication Controller")
	if err := bluegreen.createReplicationController(); err != nil {
		logger.Println(err.Error())
		return err
	}

	logger.Println("Creating versioned service")
	service, err := bluegreen.clusterManager.CreateService()
	if err != nil {
		logger.Println(err.Error())
		return err
	}

	warnings := false

	if descriptor.Frontend != "" && len(service.Spec.Ports) > 0 {
		logger.Println("Creating HAProxy configuration...")
		if err := bluegreen.clusterManager.Config.ProxyConfigurator.CreateOrUpdateProxy(
			deployment, service, logger); err != nil {
			return err
		}

		logger.Println("Creating / Updating unversioned Service")
		persistentService, err := bluegreen.clusterManager.CreateOrUpdatePersistentService()
		if err != nil {
			logger.Println(err.Error())
			return err
		}

		//!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!
		// AFTER THIS POINT DO NOT RETURN ERRORS ANYMORE, BECAUSE THE CLEANUP WON'T SWITCH BACK TO OLD HAPROXY CONFIG !!!
		//!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!!

		if err := bluegreen.clusterManager.Config.IngressConfigurator.CreateOrUpdateProxy(
			deployment, service, persistentService, logger); err != nil {
			warnings = true
			logger.Printf("WARNING: Ingress configuration failed!\n  %v", err.Error())
		}
	} else {
		logger.Println("No frontend or no ports configured in deployment, skipping proxy configuration")

		logger.Println("Creating / Updating unversioned Service")
		_, err = bluegreen.clusterManager.CreateOrUpdatePersistentService()
		if err != nil {
			logger.Println(err.Error())
			return err
		}

	}

	bluegreen.clusterManager.Logger.Println("Cleaning up old deployments")
	bluegreen.clusterManager.CleanUpOldDeployments()

	logger.Println("Updating deployment status")
	deployment.Status = types.DEPLOYMENTSTATUS_DEPLOYED
	err = bluegreen.clusterManager.Registry.UpdateDeployment(deployment)

	if err != nil {
		warnings = true
		logger.Println("WARNING: couldn't update deployment status to DEPLOYED!")
	}

	// set status of previous deployments to undeployed
	logger.Println("Updating deployment status of old deployments")
	deployments, err := bluegreen.clusterManager.Registry.GetDeployments(deployment.Descriptor.Namespace)
	if err != nil {
		warnings = true
		logger.Println("Warning: couldn't update old deployment status to UNDEPLOYED")
	} else {
		for _, oldDeployment := range deployments {
			if oldDeployment.Id != deployment.Id &&
				oldDeployment.Status == types.DEPLOYMENTSTATUS_DEPLOYED &&
				oldDeployment.Descriptor.AppName == deployment.Descriptor.AppName {

				oldDeployment.Status = types.DEPLOYMENTSTATUS_UNDEPLOYED
				err := bluegreen.clusterManager.Registry.UpdateDeployment(oldDeployment)
				if err != nil {
					warnings = true
					logger.Println("Warning: couldn't update old deployment status to UNDEPLOYED")
				}

				err = bluegreen.clusterManager.Registry.StoreLogLine(
					oldDeployment.Descriptor.Namespace, oldDeployment.Id,
					fmt.Sprintf("Undeployed during deployment of %v\n", deployment.Id))
				if err != nil {
					logger.Println("Warning: couldn't update old deployment logs")
				}
			}
		}
	}

	logger.Println("Blue-green deployment successful")
	if warnings {
		logger.Println("But there were warning(s), see above. Try a redeployment to fix deployment statuses")
	}
	return nil
}

func (bluegreen *bluegreen) createReplicationController() error {

	descriptor := bluegreen.clusterManager.Deployment.Descriptor

	_, err := bluegreen.clusterManager.CreateReplicationController()
	if err != nil {
		return err
	}

	if descriptor.Replicas == 0 {
		return nil
	}

	if descriptor.UseHealthCheck && !descriptor.IgnoreHealthCheck {
		return bluegreen.waitForPods(bluegreen.clusterManager.Deployment.GetVersionedName(), bluegreen.clusterManager.Deployment.Version)
	} else {
		return nil
	}
}

func (bluegreen *bluegreen) waitForPods(name, version string) error {
	healthChan := make(chan bool, 1)

	bluegreen.clusterManager.Logger.Printf("Waiting up to %v seconds for pods to start and to become healthy\n", bluegreen.clusterManager.Config.HealthTimeout)

	go bluegreen.checkPods(name, version, healthChan)

	select {
	case healthy := <-healthChan:
		if healthy {
			return nil
		} else {
			return errors.New("Error while waiting for pods to become healthy")
		}
	case <-time.After(time.Duration(bluegreen.clusterManager.Config.HealthTimeout) * time.Second):
		healthChan <- false
		return errors.New("Timeout waiting for pods to become healthy")
	}

}

func (bluegreen *bluegreen) checkPods(name, version string, healthChan chan bool) {

	descriptor := bluegreen.clusterManager.Deployment.Descriptor

	for {
		select {
		case <-healthChan:
			return
		default:
			{
				selector := map[string]string{"name": name, "version": bluegreen.clusterManager.Deployment.Version}
				pods, listErr := bluegreen.clusterManager.Config.K8sClient.ListPodsWithSelector(descriptor.Namespace, selector)
				if listErr != nil {
					bluegreen.clusterManager.Logger.Printf(fmt.Sprintf("Error listing pods for new deployment: %v\n", listErr))
					healthChan <- false

					return
				}

				nrOfPods := k8s.CountRunningPods(pods.Items)

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
						bluegreen.clusterManager.Logger.Println("Deployment healthy!")
						return
					} else {
						bluegreen.clusterManager.Logger.Println("Deployment not healthy yet, retrying in 1 second")
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

	descriptor := bluegreen.clusterManager.Deployment.Descriptor

	var resp *http.Response
	var err error

	port := cluster.FindHealthcheckPort(pod)
	url := bluegreen.clusterManager.GetHealthcheckUrl(pod.Status.PodIP, port)

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
			bluegreen.clusterManager.Logger.Println("Error parsing healthcheck: " + err.Error())
			return false
		}

		return dat.Healthy
	}
}

func (bluegreen *bluegreen) logHealth(pod *v1.Pod, health string) {
	descriptor := bluegreen.clusterManager.Deployment.Descriptor
	bluegreen.clusterManager.Config.EtcdRegistry.StoreHealth(descriptor.Namespace, bluegreen.clusterManager.Deployment.Id, pod.Name, health)
}

type HealthCheckEvent struct {
	Healthy bool `json:"healthy,omitempty"`
}
