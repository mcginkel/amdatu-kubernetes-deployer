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
package environment

import (
	"strings"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"github.com/coreos/etcd/client"
	"golang.org/x/net/context"
)

type EnvironmentVarStore struct {
	etcdClient client.Client
	log        logger.Logger
}

func NewEnvironmentVarStore(etcdClient client.Client, log logger.Logger) *EnvironmentVarStore {
	return &EnvironmentVarStore{etcdClient, log}
}

func (store *EnvironmentVarStore) GetEnvironmentVars() map[string]string {
	kAPI := client.NewKeysAPI(store.etcdClient)
	result, err := kAPI.Get(context.Background(), "/deployer/environment", &client.GetOptions{Recursive: true})
	if err != nil {
		store.log.Printf("Can't read environment vars from etcd: %v\n", err.Error())
		return map[string]string{}
	}

	vars := map[string]string{}
	for _, entry := range result.Node.Nodes {
		idx := strings.LastIndex(entry.Key, "/") + 1
		key := fixEnvVarName(entry.Key[idx:len(entry.Key)])

		vars[key] = entry.Value
	}

	return vars
}

func fixEnvVarName(name string) string {
	keyName := strings.ToUpper(name)
	return strings.Replace(keyName, "-", "_", -1)
}
