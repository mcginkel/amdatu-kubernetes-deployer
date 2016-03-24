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
package deploymentregistry

import (
	"encoding/json"
	"fmt"
	"log"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/cluster"
	"github.com/coreos/etcd/client"
	"golang.org/x/net/context"
)

type DeploymentRegistry struct {
	etcdApi client.KeysAPI
}

func NewDeploymentRegistry(etcdClient *client.Client) DeploymentRegistry {
	etcdApi := client.NewKeysAPI(*etcdClient)

	return DeploymentRegistry{etcdApi}
}

func (registry *DeploymentRegistry) StoreDeployment(deploymentResult cluster.DeploymentResult) error {
	keyName := fmt.Sprintf("/deployment/%v/%v/%v", deploymentResult.Deployment.Namespace, deploymentResult.Deployment.Id, deploymentResult.Date)

	bytes, err := json.MarshalIndent(deploymentResult, "", "  ")
	if err != nil {
		return err
	}

	if _, err := registry.etcdApi.Set(context.Background(), keyName, string(bytes), nil); err != nil {
		return err
	}

	return nil
}

func (registry *DeploymentRegistry) GetDeployment(namespace string, id string) (cluster.DeploymentHistory, error) {
	keyName := fmt.Sprintf("/deployment/%v/%v", namespace, id)

	resp, err := registry.etcdApi.Get(context.Background(), keyName, &client.GetOptions{Recursive: true})
	if err != nil {
		return cluster.DeploymentHistory{}, err
	}

	return ParseDeploymentHistory(resp.Node.Nodes)

}

func ParseDeploymentHistory(nodes client.Nodes) (cluster.DeploymentHistory, error) {
	deploymentHistory := cluster.DeploymentHistory{}

	for i, node := range nodes {
		deploymentResult := cluster.DeploymentResult{}

		bytes := []byte(node.Value)
		if err := json.Unmarshal(bytes, &deploymentResult); err != nil {
			return cluster.DeploymentHistory{}, err
		}

		if i == 0 {
			deploymentHistory.Id = deploymentResult.Deployment.Id
			deploymentHistory.Namespace = deploymentResult.Deployment.Namespace
			deploymentHistory.AppName = deploymentResult.Deployment.AppName
		}

		deploymentHistory.DeploymentResults = append(deploymentHistory.DeploymentResults, deploymentResult)
	}

	return deploymentHistory, nil
}

func (registry *DeploymentRegistry) ListDeployments(namespace string) ([]cluster.DeploymentHistory, error) {
	keyName := fmt.Sprintf("/deployment/%v", namespace)

	resp, err := registry.etcdApi.Get(context.Background(), keyName, &client.GetOptions{Recursive: true})
	if err != nil {
		return []cluster.DeploymentHistory{}, err
	}

	result := []cluster.DeploymentHistory{}
	for _, node := range resp.Node.Nodes {
		deploymentHistory, err := ParseDeploymentHistory(node.Nodes)

		if err != nil {
			log.Println("Can't parse deployment descriptor: "+err.Error(), node.Value)
		} else {
			result = append(result, deploymentHistory)
		}
	}

	return result, nil
}

func (registry *DeploymentRegistry) DeleteDeployment(namespace, id string) error {
	keyName := fmt.Sprintf("/deployment/%v/%v", namespace, id)

	_, err := registry.etcdApi.Delete(context.Background(), keyName, &client.DeleteOptions{Dir: false})
	return err
}
