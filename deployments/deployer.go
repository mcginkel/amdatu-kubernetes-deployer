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
package deployments

import (
	"fmt"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/bluegreen"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/cluster"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/etcdregistry"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/helper"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"
)

type Deployer struct {
	registry *etcdregistry.EtcdRegistry
	config   helper.DeployerConfig
}

func NewDeployer(config helper.DeployerConfig) Deployer {
	return Deployer{config.EtcdRegistry, config}
}

func (d *Deployer) deploy(deployment *types.Deployment, logger logger.Logger) {

	mutexKey := deployment.Descriptor.Namespace + "-" + deployment.Descriptor.AppName
	logger.Printf("Trying to acquire mutex for %v\n", mutexKey)
	mutex := helper.GetMutex(d.config.Mutexes, mutexKey)
	mutex.Lock()
	defer mutex.Unlock()
	logger.Printf("Acquired mutex for %v\n", mutexKey)

	if err := deployment.Descriptor.SetDefaults().Validate(); err != nil {
		d.handleError(logger, deployment, "Deployment descriptor incorrect: \n %v", err.Error())
		return
	}

	logger.Printf("%v\n", deployment.Descriptor.String())

	deployer := cluster.NewDeployer(d.config, deployment, logger)
	if deployment.Version == "000" {
		rc, err := deployer.FindCurrentRc()
		if err != nil {
			d.handleError(logger, deployment, "Error getting replication controllers for determining next version: %v", err.Error())
			return
		} else if len(rc) == 0 {
			deployer.Deployment.Version = "1"
		} else {

			// sometimes we have orphaned RCs, sort them out
			var activeRcs = []v1.ReplicationController{}
			for _, ctrl := range rc {
				if ctrl.DeletionTimestamp == nil {
					activeRcs = append(activeRcs, ctrl)
				} else {
					logger.Printf("Note: found orphaned replication controller %v, will try to finally delete it...\n", ctrl.Name)
					deployer.K8client.ReplicationControllers(ctrl.Namespace).Delete(ctrl.Name, &meta.DeleteOptions{})
				}
			}

			if len(activeRcs) == 0 {
				deployer.Deployment.Version = "1"
			} else if len(activeRcs) > 1 {
				d.handleError(logger, deployment, "Could not determine next deployment version, more than a singe Replication Controller found")
				return
			} else {
				var ctrl = activeRcs[0]
				logger.Println(ctrl.Name)
				versionString := ctrl.Labels["version"]
				newVersion, err := cluster.DetermineNewVersion(versionString)
				if err != nil {
					d.handleError(logger, deployment, "Could not determine next deployment version based on current version %v", err.Error())
					return
				} else {
					logger.Printf("New deployment version: %v", newVersion)
					deployer.Deployment.Version = newVersion
				}
			}
		}
	}

	var err error
	deployer.Deployment.Descriptor.Environment, err = d.registry.GetEnvironmentVars()
	if err != nil {
		logger.Println("No environment vars found")
	}

	var deploymentError error

	/*Check if namespace has the current version deployed
	If so, switch to redeployer
	*/

	logger.Println("Checking for existing service...")
	_, err = deployer.K8client.Services(deployment.Descriptor.Namespace).Get(deployer.CreateRcName(), meta.GetOptions{})

	if err != nil {
		logger.Println("No existing service found, starting deployment")

		switch deployment.Descriptor.DeploymentType {
		case "blue-green":
			deploymentError = bluegreen.NewBlueGreen(deployer).Deploy()
		default:
			d.handleError(logger, deployment, "Unknown type of deployment: %v", deployment.Descriptor.DeploymentType)
			return
		}
	} else {
		// TODO handle redeployment with same version?!
		d.handleError(logger, deployment, "Existing service found, this version is already deployed. Exiting deployment.")
		return
	}

	if deploymentError == nil {
		deployment.Status = types.DEPLOYMENTSTATUS_DEPLOYED
		d.registry.UpdateDeployment(deployment)

		// set status of previous deployments to undeployed
		deployments, err := d.registry.GetDeployments(deployment.Descriptor.Namespace)
		if err != nil {
			logger.Println("Error updating old deployments: " + err.Error())
			return
		}

		for _, oldDeployment := range deployments {
			if oldDeployment.Id != deployment.Id &&
				oldDeployment.Status == types.DEPLOYMENTSTATUS_DEPLOYED &&
				oldDeployment.Descriptor.AppName == deployment.Descriptor.AppName {

				oldDeployment.Status = types.DEPLOYMENTSTATUS_UNDEPLOYED
				d.registry.UpdateDeployment(oldDeployment)
				d.registry.StoreLogLine(oldDeployment.Descriptor.Namespace, oldDeployment.Id, fmt.Sprintf("Undeployed during deployment of %v\n", deployment.Id))
			}
		}

	} else {
		d.handleError(logger, deployment, "Deployment failed! %v\n", deploymentError.Error())
		deployer.CleanupFailedDeployment()
	}
}

func (d *Deployer) handleError(logger logger.Logger, deployment *types.Deployment, msg string, args ...interface{}) {
	message := fmt.Sprintf(msg, args...)
	logger.Println(message)
	deployment.Status = types.DEPLOYMENTSTATUS_FAILURE
	d.registry.UpdateDeployment(deployment)
}
