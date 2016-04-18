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
package proxies

import (
	"encoding/json"
	"fmt"
	"github.com/coreos/etcd/client"
	"golang.org/x/net/context"
	"log"
)

type ProxyConfigurator struct {
	etcdClient client.Client
}

type BackendServer struct {
	IPAddress          string
	Port               int32
	CompressionEnabled bool
}

type Frontend struct {
	Hostname  string
	Type      string
	BackendId string
}

func NewProxyConfigurator(etcdClient client.Client) *ProxyConfigurator {
	return &ProxyConfigurator{etcdClient}
}

func (proxyConfigurator *ProxyConfigurator) FrontendExistsForDeployment(deploymentName string) bool {
	frontendKeys := proxyConfigurator.getFrontendKeysForDeployment(deploymentName)
	return len(frontendKeys) > 0
}

func (proxyConfigurator *ProxyConfigurator) DeleteFrontendForDeployment(deploymentName string) {

	frontendKeys := proxyConfigurator.getFrontendKeysForDeployment(deploymentName)

	kAPI := client.NewKeysAPI(proxyConfigurator.etcdClient)
	for _, key := range frontendKeys {
		if _, err := kAPI.Delete(context.Background(), key, nil); err != nil {
			log.Printf("Error deleting frontend %v", key)
		}
	}
}

func (proxyConfigurator *ProxyConfigurator) getFrontendKeysForDeployment(deploymentName string) []string {
	keys := []string{}

	kAPI := client.NewKeysAPI(proxyConfigurator.etcdClient)
	result, err := kAPI.Get(context.Background(), "/proxy/frontends", &client.GetOptions{})
	if err != nil {
		fmt.Println("Error listing frontends, now assuming no frontend exists")
		return keys
	}

	for _, entry := range result.Node.Nodes {
		value := Frontend{}
		json.Unmarshal([]byte(entry.Value), &value)

		if value.BackendId == deploymentName {
			keys = append(keys, entry.Key)
		}
	}
	return keys
}

func (proxyConfigurator *ProxyConfigurator) AddBackendServer(deploymentName string, ip string, port int32, useCompression bool) error {
	kAPI := client.NewKeysAPI(proxyConfigurator.etcdClient)
	if err := prepareBaseConfig(kAPI); err != nil {
		return err
	}

	value := BackendServer{
		IPAddress:          ip,
		Port:               port,
		CompressionEnabled: useCompression,
	}

	bytes, err := json.Marshal(value)
	if err != nil {
		return err
	}

	etcdKey := fmt.Sprintf("/proxy/backends/%v/%v", deploymentName, ip)
	log.Printf("Registering backend %v for server %v:%v\n", etcdKey, ip, port)
	if _, err := kAPI.Set(context.Background(), etcdKey, string(bytes), nil); err != nil {
		return err
	}

	return nil
}

func (proxyConfigurator *ProxyConfigurator) DeleteDeployment(deploymentName string) {

	proxyConfigurator.DeleteFrontendForDeployment(deploymentName)

	kAPI := client.NewKeysAPI(proxyConfigurator.etcdClient)
	keyName := fmt.Sprintf("/proxy/backends/%v", deploymentName)
	if _, err := kAPI.Delete(context.Background(), keyName, &client.DeleteOptions{Recursive: true}); err != nil {
		log.Printf("Key %v not found, nothing deleted", keyName)
	}
}

func (proxyConfigurator *ProxyConfigurator) CreateFrontEnd(frontend *Frontend) (string, error) {
	kAPI := client.NewKeysAPI(proxyConfigurator.etcdClient)
	if err := prepareBaseConfig(kAPI); err != nil {
		return "", err
	}

	key := fmt.Sprintf("/proxy/frontends/%v", frontend.Hostname)
	resp, _ := kAPI.Get(context.Background(), key, nil)

	if resp != nil {
		log.Printf("Frontend %v already exists, skipping creation\n", key)
		return key, nil
	}

	bytes, err := json.Marshal(frontend)
	if err != nil {
		return "", err
	}

	if _, err := kAPI.Set(context.Background(), key, string(bytes), nil); err != nil {
		log.Println("Error creating proxy frontend ", err)
		return "", err
	}

	log.Printf("Created proxy frontend %v", key)

	return key, nil
}

func (proxyConfigurator *ProxyConfigurator) SwitchBackend(frontendName string, newBackendName string) error {

	kAPI := client.NewKeysAPI(proxyConfigurator.etcdClient)

	key := fmt.Sprintf("/proxy/frontends/%v", frontendName)

	value := Frontend{}
	resp, _ := kAPI.Get(context.Background(), key, nil)
	if err := json.Unmarshal([]byte(resp.Node.Value), &value); err != nil {
		return err
	}

	value.BackendId = newBackendName
	bytes, err := json.Marshal(value)
	if err != nil {
		return err
	}

	if _, err := kAPI.Set(context.Background(), key, string(bytes), nil); err != nil {
		return err
	}

	return nil
}

func (proxyConfigurator *ProxyConfigurator) DeleteBackendServer(deploymentName string, ip string) {
	kAPI := client.NewKeysAPI(proxyConfigurator.etcdClient)
	keyName := fmt.Sprintf("/proxy/backends/%v/%v", deploymentName, ip)
	if _, err := kAPI.Delete(context.Background(), keyName, nil); err != nil {
		log.Printf("Key %v not found, nothing deleted", keyName)
	}
}

func prepareBaseConfig(kAPI client.KeysAPI) error {
	_, err := kAPI.Get(context.Background(), "/proxy", nil)

	if err != nil {
		log.Println("/proxy doesn't exists, creating")

		if _, err = kAPI.Set(context.Background(), "/proxy", "", &client.SetOptions{Dir: true}); err != nil {
			log.Printf("Error creating proxy base config: %v", err)
			return err
		}

	}

	return nil
}
