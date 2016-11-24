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
	"bitbucket.org/amdatulabs/amdatu-kubernetes-go/api/v1"
	k8sClient "bitbucket.org/amdatulabs/amdatu-kubernetes-go/client"
)

type Undeployer struct {
	registry  *etcdregistry.EtcdRegistry
	config    helper.DeployerConfig
	k8sclient k8sClient.Client
}

func NewUndeployer(config helper.DeployerConfig) *Undeployer {

	k8sClient := k8sClient.NewClient(config.K8sUrl, config.K8sUsername, config.K8sPassword)
	return &Undeployer{config.EtcdRegistry, config, k8sClient}

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
	logger.Printf("Trying to acquire mutex for %v\n", mutexKey)
	mutex := helper.GetMutex(undeployer.config.Mutexes, mutexKey)
	mutex.Lock()
	defer mutex.Unlock()
	logger.Printf("Acquired mutex for %v\n", mutexKey)

	logger.Printf("Starting undeployment of application %v in namespace %v\n", deployment.Descriptor.AppName, deployment.Descriptor.Namespace)

	controllers, err := undeployer.getReplicationControllers(deployment, logger)
	if err != nil {
		undeployer.handleError(logger, deployment, "Error getting controllers: %v\n", err.Error())
		return
	}

	var success = true
	for _, controller := range controllers {
		backend := deployment.Descriptor.Namespace + "-" + controller.ObjectMeta.Name
		undeployer.deleteProxy(backend, logger)
	}

	if err = undeployer.deleteServices(deployment, logger); err != nil {
		undeployer.handleError(logger, deployment, "Error deleting services: %v\n", err.Error())
		success = false
	}

	for _, controller := range controllers {
		if helper.ShutdownReplicationController(&controller, &undeployer.k8sclient, logger); err != nil {
			undeployer.handleError(logger, deployment, "Error deleting controller: %v\n", err.Error())
			success = false
		}
	}

	if success {
		logger.Println("Deployment %v undeployed.", deployment.Id)
		if deleteDeployment {
			undeployer.deleteDeployment(deployment, logger)
		}
	}
}

func (undeployer *Undeployer) deleteDeployment(deployment *types.Deployment, logger logger.Logger) {
	logger.Println("Deleting deployment %v.", deployment.Id)
	err := undeployer.registry.DeleteDeployment(deployment.Descriptor.Namespace, deployment.Id)
	if err != nil {
		undeployer.handleError(logger, deployment, "Error deleting deployment in etcd, id %v: %v", deployment.Id, err)
		return
	}
}

func (undeployer *Undeployer) getReplicationControllers(deployment *types.Deployment, logger logger.Logger) ([]v1.ReplicationController, error) {
	logger.Printf("Getting replication controllers\n")

	labels := map[string]string{"app": deployment.Descriptor.AppName}
	rcList, err := undeployer.k8sclient.ListReplicationControllersWithLabel(deployment.Descriptor.Namespace, labels)
	if err != nil {
		return []v1.ReplicationController{}, err
	}
	return rcList.Items, nil
}

func (undeployer *Undeployer) deleteProxy(backend string, logger logger.Logger) {
	logger.Printf("Deleting proxy backend for %v\n", backend)
	undeployer.config.ProxyConfigurator.DeleteDeployment(backend, logger)

	logger.Printf("Deleting proxy frontend for %v\n", backend)
	undeployer.config.ProxyConfigurator.DeleteFrontendForDeployment(backend, logger)
}

func (undeployer *Undeployer) deleteServices(deployment *types.Deployment, logger logger.Logger) error {
	labels := map[string]string{"app": deployment.Descriptor.AppName}
	servicelist, err := undeployer.k8sclient.ListServicesWithLabel(deployment.Descriptor.Namespace, labels)
	if err != nil {
		return err
	}
	for _, service := range servicelist.Items {
		logger.Printf("Deleting service %v\n", service.ObjectMeta.Name)
		err := undeployer.k8sclient.DeleteService(deployment.Descriptor.Namespace, service.ObjectMeta.Name)
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
