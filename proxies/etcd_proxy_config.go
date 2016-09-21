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
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"strings"
	"time"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	"github.com/coreos/etcd/client"
	"golang.org/x/net/context"
)

type ProxyConfigurator struct {
	etcdClient  client.Client
	RestUrl     string
	ProxyReload int
	logger      logger.Logger
}

type BackendServer struct {
	IPAddress           string
	Port                int32
	CompressionEnabled  bool
	AdditionHttpHeaders []types.HttpHeader
}

type Frontend struct {
	Hostname  string
	Type      string
	BackendId string
}

func NewProxyConfigurator(etcdClient client.Client, restUrl string, proxyReload int, logger logger.Logger) *ProxyConfigurator {
	return &ProxyConfigurator{etcdClient, restUrl, proxyReload, logger}
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
			proxyConfigurator.logger.Printf("Error deleting frontend %v", key)
		}
	}
}

func (proxyConfigurator *ProxyConfigurator) getFrontendKeysForDeployment(deploymentName string) []string {
	keys := []string{}

	kAPI := client.NewKeysAPI(proxyConfigurator.etcdClient)
	result, err := kAPI.Get(context.Background(), "/proxy/frontends", &client.GetOptions{})
	if err != nil {
		proxyConfigurator.logger.Println("Error listing frontends, now assuming no frontend exists")
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

func (proxyConfigurator *ProxyConfigurator) AddBackendServer(deploymentName string, ip string, port int32,
	useCompression bool, additionHttpHeaders []types.HttpHeader) error {
	kAPI := client.NewKeysAPI(proxyConfigurator.etcdClient)
	if err := prepareBaseConfig(kAPI); err != nil {
		return err
	}

	value := BackendServer{
		IPAddress:           ip,
		Port:                port,
		CompressionEnabled:  useCompression,
		AdditionHttpHeaders: additionHttpHeaders,
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

func (proxyConfigurator *ProxyConfigurator) WaitForBackend(newBackendName string) error {
	if proxyConfigurator.RestUrl == "" {
		proxyConfigurator.logger.Printf("Sleeping for %v seconds for proxy to reload...\n", proxyConfigurator.ProxyReload)
		time.Sleep(time.Second * time.Duration(proxyConfigurator.ProxyReload))
	} else {
		proxyConfigurator.logger.Println("Waiting for proxy to reload...")

		successChan := make(chan bool)
		timeoutChan := make(chan bool, 2) // don't block if we timeout, but monitorBackend still waits for connection

		go proxyConfigurator.monitorBackend(newBackendName, successChan, timeoutChan)

		select {
		case success := <-successChan:
			if success {
				return nil
			} else {
				return errors.New("Error getting proxy status")
			}
		case <-time.After(time.Second * time.Duration(proxyConfigurator.ProxyReload)):
			timeoutChan <- true
			return errors.New("Waiting for proxy to get backend available timed out")
		}
	}

	return nil
}

func (proxyConfigurator *ProxyConfigurator) monitorBackend(newBackendName string, successChan chan bool, timeoutChan chan bool) {
	url := fmt.Sprintf("%s/stats/backend/%s/status", proxyConfigurator.RestUrl, newBackendName)

	for {
		select {
		case <-timeoutChan:
			return
		default:
			resp, err := http.Get(url)
			if err != nil {
				proxyConfigurator.logger.Printf("Error checking proxy backend: %v\n", err)
				successChan <- false
				return
			}

			bytes, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				proxyConfigurator.logger.Printf("Error checking proxy backend: %v\n", err)
				successChan <- false
				resp.Body.Close()
				return
			}

			resp.Body.Close()

			body := string(bytes)
			if strings.TrimSpace(body) == `"UP"` {
				successChan <- true
				return
			} else {
				proxyConfigurator.logger.Printf("Invalid status for proxy backend: %v\n", strings.TrimSpace(body))
			}

			time.Sleep(time.Second * 1)
		}

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
