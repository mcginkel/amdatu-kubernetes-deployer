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
package helper

import (
	"sync"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/etcdregistry"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/k8s"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/proxies"
)

type DeployerConfig struct {
	HealthTimeout       int64
	K8sClient           *k8s.K8sClient
	EtcdRegistry        *etcdregistry.EtcdRegistry
	ProxyConfigurator   *proxies.ProxyConfigurator
	IngressConfigurator *proxies.IngressConfigurator
	Mutexes             map[string]*sync.Mutex
}
