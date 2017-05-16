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
	"net/http"
	"strconv"
	"time"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/k8s"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	"k8s.io/client-go/pkg/api/v1"
)

type NginxStatus struct {
	k8sClient          *k8s.K8sClient
	proxyReloadTimeout int
}

type vhostTrafficStatus struct {
	UpstreamZones map[string][]struct {
		Server string `json:"server"`
		Down   bool   `json:"down"`
	} `json:"upstreamZones"`
}

func NewNginxStatus(k8sClient *k8s.K8sClient, timeout int) *NginxStatus {
	return &NginxStatus{
		k8sClient:          k8sClient,
		proxyReloadTimeout: timeout,
	}
}

func (nginx NginxStatus) WaitForProxy(deployment *types.Deployment, port int32, logger logger.Logger) error {
	logger.Println("  waiting for backend to be available...")

	successChan := make(chan bool)
	timeoutChan := make(chan bool, 2) // don't block if we timeout, but monitorBackend still waits for connection

	upstreamName := deployment.Descriptor.Namespace + "-" + deployment.GetVersionedName() + "-" + strconv.Itoa(int(port))
	go nginx.monitorProxy(upstreamName, deployment.Descriptor.Replicas, successChan, timeoutChan, logger)

	select {
	case success := <-successChan:
		if success {
			logger.Println("    ... backend is up")
			return nil
		} else {
			return errors.New("Error getting proxy status")
		}
	case <-time.After(time.Second * time.Duration(nginx.proxyReloadTimeout)):
		timeoutChan <- true
		return errors.New("    ... waiting for backend to be available timed out!")
	}

}

func (nginx NginxStatus) monitorProxy(upstreamName string, replicaCount int, successChan chan bool, timeoutChan chan bool, logger logger.Logger) {

	statusUrl, err := nginx.getNginxStatusUrl(logger)
	if err != nil {
		logger.Printf("Error getting nginx status url: %v", err.Error())
		successChan <- false
		return
	}

	for {
		select {

		case <-timeoutChan:
			return

		default:

			resp, err := http.Get(statusUrl)
			if err != nil {
				logger.Printf("Error getting nginx status: %v", err.Error())
				successChan <- false
				return
			}
			defer resp.Body.Close()

			var status vhostTrafficStatus
			err = json.NewDecoder(resp.Body).Decode(&status)
			if err != nil {
				logger.Printf("Error parsing nginx status: %v", err.Error())
				successChan <- false
				return
			}

			found := false
			for name, zones := range status.UpstreamZones {
				if name == upstreamName {
					found = true
					logger.Printf("    ... found proxy config %v", upstreamName)
					up := true
					for _, zone := range zones {
						if zone.Down {
							logger.Printf("      ... pod %v is down!", zone.Server)
						} else {
							logger.Printf("      ... pod %v is up!", zone.Server)
						}
						up = up && !zone.Down
					}
					if up && len(zones) == replicaCount {
						logger.Println("      all pods up!")
						successChan <- true
						return
					} else {
						logger.Println("      not all pods up yet!")
					}
					break
				}
			}
			if !found {
				logger.Printf("    ... didn't find proxy config %v yet", upstreamName)
			}
			logger.Println("    retrying in a moment...")
			time.Sleep(time.Second * 5)
		}

	}
}

func (nginx NginxStatus) getNginxStatusUrl(logger logger.Logger) (string, error) {
	// find nginx controller service
	svc, err := nginx.k8sClient.GetService("rti-infra", "nginx-ingress")
	if err != nil {
		logger.Printf("Error getting nginx service: %v", err.Error())
		return "", err
	}
	nginxIp := svc.Spec.ClusterIP
	nginxPort, err := nginx.findNginxStatusPort(svc.Spec.Ports)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("http://%v:%v/%v", nginxIp, nginxPort, "nginx_status/format/json"), nil
}

func (nginx NginxStatus) findNginxStatusPort(ports []v1.ServicePort) (int32, error) {
	for _, port := range ports {
		if port.Name == "status" {
			return port.TargetPort.IntVal, nil
		}
	}
	return 0, errors.New("Could not find nginx status port!")
}
