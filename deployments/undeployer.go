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

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/etcdregistry"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/helper"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	"k8s.io/client-go/pkg/api/v1"
)

type Undeployer struct {
	registry *etcdregistry.EtcdRegistry
	config   helper.DeployerConfig
}

func NewUndeployer(config helper.DeployerConfig, logger logger.Logger) *Undeployer {

	return &Undeployer{config.EtcdRegistry, config}

}

func (undeployer *Undeployer) Undeploy(deployment *types.Deployment, logger logger.Logger, deleteDeployment bool) {

	if deployment.Status != types.DEPLOYMENTSTATUS_DEPLOYED {
		if deleteDeployment {
			undeployer.deleteDeployment(deployment, logger)
		}
		return
	}

	deployment.Status = types.DEPLOYMENTSTATUS_UNDEPLOYING
	undeployer.registry.UpdateDeployment(deployment)

	mutexKey := deployment.Descriptor.Namespace + "-" + deployment.Descriptor.AppName
	logger.Printf("Trying to acquire mutex for %v", mutexKey)
	mutex := helper.GetMutex(undeployer.config.Mutexes, mutexKey)
	mutex.Lock()
	defer mutex.Unlock()
	logger.Printf("Acquired mutex for %v", mutexKey)

	logger.Printf("Starting undeployment of application %v in namespace %v", deployment.Descriptor.AppName, deployment.Descriptor.Namespace)

	var success = true

	controllers, err := undeployer.getReplicationControllers(deployment, logger)
	if err != nil {
		undeployer.handleError(logger, deployment, "Error getting controllers: %v", err.Error())
		return
	} else {
		for _, controller := range controllers {
			if undeployer.config.K8sClient.ShutdownReplicationController(&controller, logger); err != nil {
				undeployer.handleError(logger, deployment, "Error deleting controller: %v", err.Error())
				success = false
			}
		}
	}

	if err = undeployer.deleteServices(deployment, logger); err != nil {
		undeployer.handleError(logger, deployment, "Error deleting services: %v", err.Error())
		success = false
	}

	undeployer.deleteProxy(deployment, logger)

	if success {
		deployment.Status = types.DEPLOYMENTSTATUS_UNDEPLOYED
		undeployer.registry.UpdateDeployment(deployment)

		logger.Printf("Deployment %v undeployed.", deployment.Id)
		if deleteDeployment {
			undeployer.deleteDeployment(deployment, logger)
		}
	}
}

func (undeployer *Undeployer) deleteDeployment(deployment *types.Deployment, logger logger.Logger) {
	logger.Printf("Deleting deployment %v", deployment.Id)
	err := undeployer.registry.DeleteDeployment(deployment.Descriptor.Namespace, deployment.Id)
	if err != nil {
		undeployer.handleError(logger, deployment, "Error deleting deployment in etcd, id %v: %v", deployment.Id, err)
		return
	}
}

func (undeployer *Undeployer) getReplicationControllers(deployment *types.Deployment, logger logger.Logger) ([]v1.ReplicationController, error) {
	logger.Printf("Getting replication controllers\n")

	selector := map[string]string{"app": deployment.Descriptor.AppName}
	rcList, err := undeployer.config.K8sClient.ListReplicationControllersWithSelector(deployment.Descriptor.Namespace, selector)
	if err != nil {
		return []v1.ReplicationController{}, err
	}
	return rcList.Items, nil
}

func (undeployer *Undeployer) deleteProxy(deployment *types.Deployment, logger logger.Logger) {
	logger.Printf("Deleting proxy for %v", deployment.Descriptor.AppName)
	if err := undeployer.config.ProxyConfigurator.DeleteProxy(deployment, logger); err != nil {
		logger.Printf("  Error deleting HAproxy config: %v", err.Error())
	}
}

func (undeployer *Undeployer) deleteServices(deployment *types.Deployment, logger logger.Logger) error {

	selector := map[string]string{"app": deployment.Descriptor.AppName}
	servicelist, err := undeployer.config.K8sClient.ListServicesWithSelector(deployment.Descriptor.Namespace, selector)
	if err != nil {
		return err
	}

	for _, service := range servicelist.Items {
		logger.Printf("Deleting service %v\n", service.ObjectMeta.Name)
		err := undeployer.config.K8sClient.DeleteService(deployment.Descriptor.Namespace, service.Name)
		if err != nil {
			return err
		}
	}

	return nil
}

func (undeployer *Undeployer) handleError(logger logger.Logger, deployment *types.Deployment, msg string, args ...interface{}) {
	message := fmt.Sprintf(msg, args...)
	logger.Println(message)
	deployment.Status = types.DEPLOYMENTSTATUS_FAILURE
	undeployer.registry.UpdateDeployment(deployment)
}
