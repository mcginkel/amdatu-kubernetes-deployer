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
	etcdApi     client.KeysAPI
	RestUrl     string
	ProxyReload int
}

type BackendServer struct {
	IPAddress           string
	Port                int32
	CompressionEnabled  bool
	AdditionHttpHeaders []types.HttpHeader
}

type Frontend struct {
	Hostname          string
	Type              string
	BackendId         string
	RedirectWwwPrefix bool
}

func NewProxyConfigurator(etcdApi client.KeysAPI, restUrl string, proxyReload int) *ProxyConfigurator {
	return &ProxyConfigurator{etcdApi, restUrl, proxyReload}
}

func (proxyConfigurator *ProxyConfigurator) FrontendExistsForDeployment(deploymentName string, logger logger.Logger) bool {
	frontendKeys := proxyConfigurator.getFrontendKeysForDeployment(deploymentName, logger)
	return len(frontendKeys) > 0
}

func (proxyConfigurator *ProxyConfigurator) DeleteFrontendForDeployment(deploymentName string, logger logger.Logger) {

	frontendKeys := proxyConfigurator.getFrontendKeysForDeployment(deploymentName, logger)

	for _, key := range frontendKeys {
		if _, err := proxyConfigurator.etcdApi.Delete(context.Background(), key, nil); err != nil {
			logger.Printf("Error deleting frontend %v", key)
		}
	}
}

func (proxyConfigurator *ProxyConfigurator) getFrontendKeysForDeployment(deploymentName string, logger logger.Logger) []string {
	keys := []string{}

	result, err := proxyConfigurator.etcdApi.Get(context.Background(), "/proxy/frontends", &client.GetOptions{})
	if err != nil {
		logger.Println("Error listing frontends, now assuming no frontend exists")
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
	if err := proxyConfigurator.prepareBaseConfig(); err != nil {
		return err
	}

	if additionHttpHeaders != nil {
		prefixSpacesInHeaderValues(additionHttpHeaders)
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
	if _, err := proxyConfigurator.etcdApi.Set(context.Background(), etcdKey, string(bytes), nil); err != nil {
		return err
	}

	return nil
}

func (proxyConfigurator *ProxyConfigurator) DeleteDeployment(deploymentName string, logger logger.Logger) {

	proxyConfigurator.DeleteFrontendForDeployment(deploymentName, logger)

	keyName := fmt.Sprintf("/proxy/backends/%v", deploymentName)
	if _, err := proxyConfigurator.etcdApi.Delete(context.Background(), keyName, &client.DeleteOptions{Recursive: true}); err != nil {
		logger.Printf("Key %v not found, nothing deleted\n", keyName)
	}
}

func (proxyConfigurator *ProxyConfigurator) CreateFrontEnd(frontend *Frontend) (string, error) {
	if err := proxyConfigurator.prepareBaseConfig(); err != nil {
		return "", err
	}

	key := fmt.Sprintf("/proxy/frontends/%v", frontend.Hostname)
	resp, _ := proxyConfigurator.etcdApi.Get(context.Background(), key, nil)

	if resp != nil {
		// frontend exists, but there might be changed properties like redirectWww
		log.Printf("Frontend %v already exists, updating it\n", key)
		old := Frontend{}
		if err := json.Unmarshal([]byte(resp.Node.Value), &old); err != nil {
			return "", err
		}
		frontend.BackendId = old.BackendId
	}

	bytes, err := json.Marshal(frontend)
	if err != nil {
		return "", err
	}

	if _, err := proxyConfigurator.etcdApi.Set(context.Background(), key, string(bytes), nil); err != nil {
		log.Println("Error creating proxy frontend ", err)
		return "", err
	}

	log.Printf("Created proxy frontend %v", key)

	return key, nil
}

func (proxyConfigurator *ProxyConfigurator) SwitchBackend(frontendName string, newBackendName string) error {

	key := fmt.Sprintf("/proxy/frontends/%v", frontendName)

	value := Frontend{}
	resp, _ := proxyConfigurator.etcdApi.Get(context.Background(), key, nil)
	if err := json.Unmarshal([]byte(resp.Node.Value), &value); err != nil {
		return err
	}

	value.BackendId = newBackendName
	bytes, err := json.Marshal(value)
	if err != nil {
		return err
	}

	if _, err := proxyConfigurator.etcdApi.Set(context.Background(), key, string(bytes), nil); err != nil {
		return err
	}

	return nil
}

func (proxyConfigurator *ProxyConfigurator) DeleteBackendServer(deploymentName string, ip string) {
	keyName := fmt.Sprintf("/proxy/backends/%v/%v", deploymentName, ip)
	if _, err := proxyConfigurator.etcdApi.Delete(context.Background(), keyName, nil); err != nil {
		log.Printf("Key %v not found, nothing deleted", keyName)
	}
}

func (proxyConfigurator *ProxyConfigurator) WaitForBackend(newBackendName string, logger logger.Logger) error {
	if proxyConfigurator.RestUrl == "" {
		logger.Printf("Sleeping for %v seconds for proxy to reload...\n", proxyConfigurator.ProxyReload)
		time.Sleep(time.Second * time.Duration(proxyConfigurator.ProxyReload))
	} else {
		logger.Println("Waiting for proxy to reload...")

		successChan := make(chan bool)
		timeoutChan := make(chan bool, 2) // don't block if we timeout, but monitorBackend still waits for connection

		go proxyConfigurator.monitorBackend(newBackendName, successChan, timeoutChan, logger)

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

func (proxyConfigurator *ProxyConfigurator) monitorBackend(newBackendName string, successChan chan bool, timeoutChan chan bool, logger logger.Logger) {
	url := fmt.Sprintf("%s/stats/backend/%s/status", proxyConfigurator.RestUrl, newBackendName)

	for {
		select {
		case <-timeoutChan:
			return
		default:
			resp, err := http.Get(url)
			if err != nil {
				logger.Printf("Error checking proxy backend: %v\n", err)
				successChan <- false
				return
			}

			bytes, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				logger.Printf("Error checking proxy backend: %v\n", err)
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
				logger.Printf("Invalid status for proxy backend: %v\n", strings.TrimSpace(body))
			}

			time.Sleep(time.Second * 1)
		}

	}
}

func (proxyConfigurator *ProxyConfigurator) prepareBaseConfig() error {
	_, err := proxyConfigurator.etcdApi.Get(context.Background(), "/proxy", nil)

	if err != nil {
		log.Println("/proxy doesn't exists, creating")

		if _, err = proxyConfigurator.etcdApi.Set(context.Background(), "/proxy", "", &client.SetOptions{Dir: true}); err != nil {
			log.Printf("Error creating proxy base config: %v", err)
			return err
		}

	}

	return nil
}

func prefixSpacesInHeaderValues(headers []types.HttpHeader) {
	for i, header := range headers {
		value := header.Value
		// prefix spaces with \, but only if that wasn't done before
		value = strings.Replace(value, " ", "\\ ", -1)
		value = strings.Replace(value, "\\\\ ", "\\ ", -1)
		headers[i].Value = value
	}
}
