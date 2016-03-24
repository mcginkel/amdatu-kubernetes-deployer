package environment

import (
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/cluster"
	"github.com/coreos/etcd/client"
	"golang.org/x/net/context"
	"strings"
)

type EnvironmentVarStore struct {
	etcdClient client.Client
	log        cluster.Logger
}

func NewEnvironmentVarStore(etcdClient client.Client, log cluster.Logger) *EnvironmentVarStore {
	return &EnvironmentVarStore{etcdClient, log}
}

func (store *EnvironmentVarStore) GetEnvironmentVars() map[string]string {
	kAPI := client.NewKeysAPI(store.etcdClient)
	result, err := kAPI.Get(context.Background(), "/deployer/environment", &client.GetOptions{Recursive: true})
	if err != nil {
		store.log.Printf("Can't read environment vars from etcd: %v", err.Error())
		return map[string]string{}
	}

	vars := map[string]string{}
	for _, entry := range result.Node.Nodes {
		idx := strings.LastIndex(entry.Key, "/") + 1
		key := entry.Key[idx:len(entry.Key)]

		vars[key] = entry.Value
	}

	return vars
}
