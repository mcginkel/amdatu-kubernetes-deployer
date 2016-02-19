package deploymentregistry

import (
	"com.amdatu.rti.deployment/cluster"
	"encoding/json"
	"fmt"
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

func (registry *DeploymentRegistry) StoreDeployment(deployment *cluster.Deployment) error {
	keyName := fmt.Sprintf("/deployment/%v/%v/%v", deployment.Namespace, deployment.AppName, deployment.NewVersion)

	bytes, err := json.MarshalIndent(deployment, "", "  ")
	if err != nil {
		return err
	}

	if _, err := registry.etcdApi.Set(context.Background(), keyName, string(bytes), nil); err != nil {
		return err
	}

	return nil
}

func (registry *DeploymentRegistry) GetDeployment(namespace string, deploymentName string) (*cluster.Deployment, error) {
	keyName := fmt.Sprintf("/deployment/%v/%v", namespace, deploymentName)

	resp, err := registry.etcdApi.Get(context.Background(), keyName, nil)
	if err != nil {
		return &cluster.Deployment{}, err
	}

	deployment := cluster.Deployment{}
	bytes := []byte(resp.Node.Value)
	if err := json.Unmarshal(bytes, deployment); err != nil {
		return &cluster.Deployment{}, err
	}

	return &deployment, nil
}
