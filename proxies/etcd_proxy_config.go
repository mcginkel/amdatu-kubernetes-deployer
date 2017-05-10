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
	"k8s.io/client-go/pkg/api/v1"
)

type ProxyConfigurator struct {
	etcdApi     client.KeysAPI
	RestUrl     string
	ProxyReload int
}

type backendServer struct {
	IPAddress           string
	Port                int32
	CompressionEnabled  bool
	AdditionHttpHeaders []types.HttpHeader
}

type frontend struct {
	Hostname          string
	Type              string
	BackendId         string
	RedirectWwwPrefix bool
}

func NewProxyConfigurator(etcdApi client.KeysAPI, restUrl string, proxyReload int) *ProxyConfigurator {
	return &ProxyConfigurator{etcdApi, restUrl, proxyReload}
}

func (proxyConfigurator *ProxyConfigurator) CreateOrUpdateProxy(deployment *types.Deployment,
	service *v1.Service, logger logger.Logger) error {

	descriptor := deployment.Descriptor
	backendId := descriptor.Namespace + "-" + deployment.GetVersionedName()

	fe := frontend{
		Type:              "http",
		Hostname:          descriptor.Frontend,
		BackendId:         backendId,
		RedirectWwwPrefix: descriptor.RedirectWww,
	}

	logger.Println("  ... frontend")
	if err := proxyConfigurator.createFrontEnd(&fe, logger); err != nil {
		logger.Println(err.Error())
		return err
	}

	logger.Println("  ... backend")
	if err := proxyConfigurator.addBackendServer(backendId, descriptor, service, logger); err != nil {
		logger.Println(err.Error())
		return err
	}

	logger.Println("  ... waiting for backend being up")
	if err := proxyConfigurator.waitForBackend(backendId, logger); err != nil {
		logger.Println(err.Error())
		return err
	}

	logger.Println("  ... switching to new backend")
	if err := proxyConfigurator.switchBackend(descriptor.Frontend, backendId, logger); err != nil {
		logger.Println(err.Error())
		return err
	}

	return nil

}

func (proxyConfigurator *ProxyConfigurator) createFrontEnd(fe *frontend, logger logger.Logger) error {
	if err := proxyConfigurator.prepareBaseConfig(); err != nil {
		return err
	}

	key := fmt.Sprintf("/proxy/frontends/%v", fe.Hostname)
	resp, _ := proxyConfigurator.etcdApi.Get(context.Background(), key, nil)

	if resp != nil {
		// frontend exists, but there might be changed properties like redirectWww
		logger.Printf("    ... frontend for %v already exists, updating it", fe.Hostname)
		old := frontend{}
		if err := json.Unmarshal([]byte(resp.Node.Value), &old); err != nil {
			return err
		}
		fe.BackendId = old.BackendId
	}

	bytes, err := json.Marshal(fe)
	if err != nil {
		return err
	}

	if _, err := proxyConfigurator.etcdApi.Set(context.Background(), key, string(bytes), nil); err != nil {
		return err
	}

	log.Printf("    ... created / updated proxy frontend for %v", fe.Hostname)

	return nil
}

func (proxyConfigurator *ProxyConfigurator) addBackendServer(backendId string, descriptor *types.Descriptor, service *v1.Service, logger logger.Logger) error {

	if err := proxyConfigurator.prepareBaseConfig(); err != nil {
		return err
	}

	httpHeaders := descriptor.AdditionHttpHeaders
	if httpHeaders != nil {
		prefixSpacesInHeaderValues(httpHeaders)
	}

	port := selectPort(service.Spec.Ports)

	be := backendServer{
		IPAddress:           service.Spec.ClusterIP,
		Port:                port,
		CompressionEnabled:  descriptor.UseCompression,
		AdditionHttpHeaders: httpHeaders,
	}

	bytes, err := json.Marshal(be)
	if err != nil {
		return err
	}

	etcdKey := fmt.Sprintf("/proxy/backends/%v/%v", backendId, be.IPAddress)
	if _, err := proxyConfigurator.etcdApi.Set(context.Background(), etcdKey, string(bytes), nil); err != nil {
		return err
	}

	logger.Printf("    ... created proxy backend for %v", service.Name)

	return nil
}

func (proxyConfigurator *ProxyConfigurator) waitForBackend(newBackendName string, logger logger.Logger) error {
	if proxyConfigurator.RestUrl == "" {
		logger.Printf("    ... sleeping for %v seconds for proxy to reload", proxyConfigurator.ProxyReload)
		time.Sleep(time.Second * time.Duration(proxyConfigurator.ProxyReload))
	} else {
		logger.Println("    waiting for backend to be available...")

		successChan := make(chan bool)
		timeoutChan := make(chan bool, 2) // don't block if we timeout, but monitorBackend still waits for connection

		go proxyConfigurator.monitorBackend(newBackendName, successChan, timeoutChan, logger)

		select {
		case success := <-successChan:
			if success {
				logger.Println("    ... backend is up")
				return nil
			} else {
				return errors.New("Error getting proxy status")
			}
		case <-time.After(time.Second * time.Duration(proxyConfigurator.ProxyReload)):
			timeoutChan <- true
			return errors.New("    waiting for backend to be available timed out!")
		}
	}

	return nil
}

func (proxyConfigurator *ProxyConfigurator) switchBackend(frontendName string, newBackendName string, logger logger.Logger) error {

	key := fmt.Sprintf("/proxy/frontends/%v", frontendName)

	fe := frontend{}
	resp, _ := proxyConfigurator.etcdApi.Get(context.Background(), key, nil)
	if err := json.Unmarshal([]byte(resp.Node.Value), &fe); err != nil {
		return err
	}

	fe.BackendId = newBackendName
	bytes, err := json.Marshal(fe)
	if err != nil {
		return err
	}

	if _, err := proxyConfigurator.etcdApi.Set(context.Background(), key, string(bytes), nil); err != nil {
		return err
	}

	logger.Println("    ... switched backend")
	return nil
}

func (proxyConfigurator *ProxyConfigurator) DeleteProxy(deployment *types.Deployment, logger logger.Logger) error {
	err1 := proxyConfigurator.deleteFrontend(deployment, logger)
	backendId := deployment.Descriptor.Namespace + "-" + deployment.GetVersionedName()
	err2 := proxyConfigurator.DeleteBackend(backendId, logger)

	errmsg := ""
	if err1 != nil {
		errmsg = err1.Error()
	}
	if err2 != nil {
		errmsg += ", " + err2.Error()
	}
	if len(errmsg) > 1 {
		return errors.New(errmsg)
	}
	return nil
}

func (proxyConfigurator *ProxyConfigurator) deleteFrontend(deployment *types.Deployment, logger logger.Logger) error {
	key := fmt.Sprintf("/proxy/frontends/%v", deployment.Descriptor.Frontend)
	if _, err := proxyConfigurator.etcdApi.Delete(context.Background(), key, nil); err != nil {
		logger.Printf("Error deleting proxy frontend %v: %v", deployment.Descriptor.Frontend, err.Error())
		return err
	}
	return nil
}

func (proxyConfigurator *ProxyConfigurator) DeleteBackend(backendId string, logger logger.Logger) error {
	key := fmt.Sprintf("/proxy/backends/%v", backendId)
	if _, err := proxyConfigurator.etcdApi.Delete(context.Background(), key, &client.DeleteOptions{Recursive: true}); err != nil {
		logger.Printf("Error deleting proxy backend %v: %v", backendId, err.Error())
		return err
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

func selectPort(ports []v1.ServicePort) int32 {
	if len(ports) > 1 {
		for _, port := range ports {
			if port.Name != "healthcheck" {
				return port.Port
			}
		}
	}

	return ports[0].Port
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
