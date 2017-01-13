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
package etcdregistry

import (
	"encoding/json"
	"fmt"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-go/api/v1"
	"github.com/coreos/etcd/client"
	etcd "github.com/coreos/etcd/client"
	"golang.org/x/net/context"

	"errors"
	"strings"
	"time"
)

const (
	PATH_DESCRIPTORS = "/deployer/descriptors/"
	PATH_DEPLOYMENTS = "/deployer/deployments/"
	PATH_ENVIRONMENT = "/deployer/environment/"
	PATH_HEALTHDATA  = "/deployer/healthcheckdata/"
	PATH_LOGS        = "/deployer/logs/"
)

var (
	ErrDescriptorNotFound = errors.New("descriptor not found!")
	ErrDeploymentNotFound = errors.New("deployment not found!")
)

type EtcdRegistry struct {
	etcdApi etcd.KeysAPI
}

func NewEtcdRegistry(etcdApi etcd.KeysAPI) *EtcdRegistry {
	return &EtcdRegistry{etcdApi}
}

func (registry *EtcdRegistry) CreateDeployment(deployment *types.Deployment) error {
	return registry.storeDeployment(deployment, true, false)
}

func (registry *EtcdRegistry) CreateDeploymentWithoutTimestamps(deployment *types.Deployment) error {
	return registry.storeDeployment(deployment, true, true)
}

func (registry *EtcdRegistry) UpdateDeployment(deployment *types.Deployment) error {
	return registry.storeDeployment(deployment, false, false)
}

func (registry *EtcdRegistry) storeDeployment(deployment *types.Deployment, isNew bool, skipSettingTimestamps bool) error {
	if !skipSettingTimestamps {
		ts := time.Now().Format(time.RFC3339)
		if isNew {
			deployment.Created = ts
		}
		deployment.LastModified = ts
	}
	return registry.storeJson(PATH_DEPLOYMENTS, deployment.Descriptor.Namespace, deployment.Descriptor.AppName, deployment.Id, deployment, isNew)
}

func (registry *EtcdRegistry) GetDeployments(namespace string) ([]*types.Deployment, error) {
	keyName := PATH_DEPLOYMENTS + namespace

	resp, err := registry.etcdApi.Get(context.Background(), keyName, &client.GetOptions{Recursive: true})
	if err != nil {
		if strings.Contains(err.Error(), "Key not found") {
			return nil, ErrDeploymentNotFound
		}
		return nil, err
	}

	return parseDeployments(resp.Node.Nodes)

}

func (registry *EtcdRegistry) GetDeploymentById(namespace string, id string) (*types.Deployment, error) {
	deployments, err := registry.GetDeployments(namespace)
	if err != nil {
		return &types.Deployment{}, err
	}
	for _, deployment := range deployments {
		if deployment.Id == id {
			return deployment, nil
		}

	}
	return &types.Deployment{}, ErrDeploymentNotFound
}

func (registry *EtcdRegistry) DeleteDeployment(namespace string, id string) error {
	deployment, err := registry.GetDeploymentById(namespace, id)
	if err != nil {
		return err
	}
	keyName := fmt.Sprintf("%v%v/%v/%v", PATH_DEPLOYMENTS, namespace, deployment.Descriptor.AppName, id)
	_, err = registry.etcdApi.Delete(context.Background(), keyName, &client.DeleteOptions{Recursive: true})

	keyName = fmt.Sprintf("%v%v/%v", PATH_HEALTHDATA, namespace, id)
	_, err = registry.etcdApi.Delete(context.Background(), keyName, &client.DeleteOptions{Recursive: true})

	keyName = fmt.Sprintf("%v%v/%v", PATH_LOGS, namespace, id)
	_, err = registry.etcdApi.Delete(context.Background(), keyName, &client.DeleteOptions{Recursive: true})

	return err
}

func (registry *EtcdRegistry) CreateDescriptor(descriptor *types.Descriptor) error {
	return registry.storeDescriptor(descriptor, true, false)
}

func (registry *EtcdRegistry) CreateDescriptorWithoutTimestamps(descriptor *types.Descriptor) error {
	return registry.storeDescriptor(descriptor, true, true)
}

func (registry *EtcdRegistry) UpdateDescriptor(descriptor *types.Descriptor) error {
	return registry.storeDescriptor(descriptor, false, false)
}

func (registry *EtcdRegistry) storeDescriptor(descriptor *types.Descriptor, isNew bool, skipSettingTimestamps bool) error {
	if !skipSettingTimestamps {
		ts := time.Now().Format(time.RFC3339)
		if isNew {
			descriptor.Created = ts
		}
		descriptor.LastModified = ts
	}
	return registry.storeJson(PATH_DESCRIPTORS, descriptor.Namespace, descriptor.AppName, descriptor.Id, descriptor, isNew)
}

func (registry *EtcdRegistry) GetDescriptors(namespace string) ([]*types.Descriptor, error) {
	keyName := PATH_DESCRIPTORS + namespace

	resp, err := registry.etcdApi.Get(context.Background(), keyName, &client.GetOptions{Recursive: true})
	if err != nil {
		if strings.Contains(err.Error(), "Key not found") {
			return nil, ErrDescriptorNotFound
		}
		return nil, err
	}

	return parseDescriptors(resp.Node.Nodes)
}

func (registry *EtcdRegistry) GetDescriptorById(namespace string, id string) (*types.Descriptor, error) {
	descriptors, err := registry.GetDescriptors(namespace)
	if err != nil {
		return &types.Descriptor{}, err
	}
	for _, descriptor := range descriptors {
		if descriptor.Id == id {
			return descriptor, nil
		}

	}
	return &types.Descriptor{}, ErrDescriptorNotFound
}

func (registry *EtcdRegistry) DeleteDescriptor(namespace string, id string) error {
	descriptor, err := registry.GetDescriptorById(namespace, id)
	if err != nil {
		return err
	}
	keyName := fmt.Sprintf("%v%v/%v/%v", PATH_DESCRIPTORS, namespace, descriptor.AppName, id)
	_, err = registry.etcdApi.Delete(context.Background(), keyName, &client.DeleteOptions{Recursive: true})
	return err
}

func (registry *EtcdRegistry) GetNamespaces() ([]string, error) {
	var namespaces []string
	resp, err := registry.etcdApi.Get(context.Background(), PATH_DESCRIPTORS, &client.GetOptions{Recursive: true})
	if err != nil {
		return nil, err
	}
	for _, node := range resp.Node.Nodes {
		key := node.Key
		index := strings.LastIndex(key, "/")
		namespace := key[index+1:]
		namespaces = append(namespaces, namespace)
	}
	return namespaces, nil
}

func (registry *EtcdRegistry) storeJson(basePath string, namespace string, appname string, id string, object interface{}, isNew bool) error {
	keyName := fmt.Sprintf("%v%v/%v/%v", basePath, namespace, appname, id)

	bytes, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		return err
	}

	// check if correctly creating or updating
	var prevExists client.PrevExistType
	if isNew {
		prevExists = etcd.PrevNoExist
	} else {
		prevExists = etcd.PrevExist
	}
	options := etcd.SetOptions{PrevExist: prevExists}

	if _, err := registry.etcdApi.Set(context.Background(), keyName, string(bytes), &options); err != nil {
		return err
	}

	return nil
}

func parseDeployments(nodes client.Nodes) ([]*types.Deployment, error) {
	var deployments []*types.Deployment

	for _, appNode := range nodes {
		for _, deploymentNode := range appNode.Nodes {
			deployment := &types.Deployment{}

			bytes := []byte(deploymentNode.Value)
			if err := json.Unmarshal(bytes, deployment); err != nil {
				return []*types.Deployment{}, err
			}

			cleanDescriptor(deployment.Descriptor)

			deployments = append(deployments, deployment)
		}
	}

	return deployments, nil
}

func parseDescriptors(nodes client.Nodes) ([]*types.Descriptor, error) {

	var descriptors []*types.Descriptor

	for _, appNode := range nodes {
		for _, descriptorNode := range appNode.Nodes {
			descriptor := &types.Descriptor{}

			bytes := []byte(descriptorNode.Value)
			if err := json.Unmarshal(bytes, descriptor); err != nil {
				return nil, err
			}

			cleanDescriptor(descriptor)

			descriptors = append(descriptors, descriptor)
		}
	}

	return descriptors, nil
}

func cleanDescriptor(descriptor *types.Descriptor) {
	// remove env vars which were added by deployer
	keysToRemove := make([]string, 0)
	keysToRemove = append(keysToRemove, "APP_NAME")
	keysToRemove = append(keysToRemove, "POD_NAMESPACE")
	keysToRemove = append(keysToRemove, "APP_VERSION")
	keysToRemove = append(keysToRemove, "POD_NAME")
	for key := range descriptor.Environment {
		keysToRemove = append(keysToRemove, key)
	}
	for i, container := range descriptor.PodSpec.Containers {
		newEnv := make([]v1.EnvVar, 0)
		for _, env := range container.Env {
			keep := true
			for _, key := range keysToRemove {
				if env.Name == key {
					keep = false
					break
				}
			}
			if keep {
				newEnv = append(newEnv, env)
			}
		}
		descriptor.PodSpec.Containers[i].Env = newEnv
	}
	// remove environment, that's internal information only
	descriptor.Environment = nil
}

func (registry *EtcdRegistry) GetEnvironmentVars() (map[string]string, error) {
	result, err := registry.etcdApi.Get(context.Background(), PATH_ENVIRONMENT, &client.GetOptions{Recursive: true})
	if err != nil {
		return nil, err
	}

	vars := map[string]string{}
	for _, entry := range result.Node.Nodes {
		idx := strings.LastIndex(entry.Key, "/") + 1
		key := fixEnvVarName(entry.Key[idx:len(entry.Key)])

		vars[key] = entry.Value
	}

	return vars, nil
}

func fixEnvVarName(name string) string {
	keyName := strings.ToUpper(name)
	return strings.Replace(keyName, "-", "_", -1)
}

func (registry *EtcdRegistry) StoreHealth(namespace string, deploymentId string, podName string, health string) error {
	keyName := fmt.Sprintf("%v%v/%v/%v", PATH_HEALTHDATA, namespace, deploymentId, podName)
	_, err := registry.etcdApi.Set(context.Background(), keyName, health, nil)
	return err
}

func (registry *EtcdRegistry) GetHealth(namespace string, deploymentId string) ([]types.HealthData, error) {
	keyName := fmt.Sprintf("%v%v/%v", PATH_HEALTHDATA, namespace, deploymentId)
	response, err := registry.etcdApi.Get(context.Background(), keyName, nil)
	if err != nil {
		return nil, err
	}
	results := []types.HealthData{}
	for _, pod := range response.Node.Nodes {
		podName := pod.Key[strings.LastIndex(pod.Key, "/")+1:]
		results = append(results, types.HealthData{PodName: podName, Value: pod.Value})
	}
	return results, nil
}

func (registry *EtcdRegistry) StoreLogLine(namespace string, deploymentId string, logLine string) error {

	if !strings.HasSuffix(logLine, "\n") {
		logLine += "\n"
	}

	var newLog string

	oldLog, _, err := registry.GetLogs(namespace, deploymentId)
	if err != nil {
		newLog = logLine
	} else {
		newLog = oldLog + logLine
	}

	keyName := fmt.Sprintf("%v%v/%v", PATH_LOGS, namespace, deploymentId)
	_, err = registry.etcdApi.Set(context.Background(), keyName, newLog, nil)
	return err
}

func (registry *EtcdRegistry) GetLogs(namespace string, deploymentId string) (string, uint64, error) {
	keyName := fmt.Sprintf("%v%v/%v", PATH_LOGS, namespace, deploymentId)
	response, err := registry.etcdApi.Get(context.Background(), keyName, nil)
	if err != nil {
		return "", 0, err
	}
	return response.Node.Value, response.Index, nil
}

func (registry *EtcdRegistry) NextLogs(namespace string, deploymentId string, index uint64) (string, uint64, error) {
	keyName := fmt.Sprintf("%v%v/%v", PATH_LOGS, namespace, deploymentId)
	watcher := registry.etcdApi.Watcher(keyName, &etcd.WatcherOptions{AfterIndex: index})
	c, _ := context.WithTimeout(context.Background(), 5*time.Second)
	response, err := watcher.Next(c)
	if err != nil {
		return "", 0, err
	}
	return response.Node.Value, response.Index, nil
}
