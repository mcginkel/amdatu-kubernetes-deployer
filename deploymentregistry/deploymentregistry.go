package deploymentregistry

import (
	"com.amdatu.rti.deployment/cluster"
	"encoding/json"
	"fmt"
	"github.com/coreos/etcd/client"
	"golang.org/x/net/context"
	"log"
)

type DeploymentRegistry struct {
	etcdApi client.KeysAPI
}

func NewDeploymentRegistry(etcdClient *client.Client) DeploymentRegistry {
	etcdApi := client.NewKeysAPI(*etcdClient)

	return DeploymentRegistry{etcdApi}
}

func (registry *DeploymentRegistry) StoreDeployment(deployment *cluster.Deployment) error {
	keyName := fmt.Sprintf("/deployment/%v/%v", deployment.Namespace, deployment.Id)

	bytes, err := json.MarshalIndent(deployment, "", "  ")
	if err != nil {
		return err
	}

	if _, err := registry.etcdApi.Set(context.Background(), keyName, string(bytes), nil); err != nil {
		return err
	}

	return nil
}

func (registry *DeploymentRegistry) GetDeployment(namespace string, id string) (*cluster.Deployment, error) {
	keyName := fmt.Sprintf("/deployment/%v/%v", namespace, id)

	resp, err := registry.etcdApi.Get(context.Background(), keyName, nil)
	if err != nil {
		return &cluster.Deployment{}, err
	}

	return ParseDeployment(resp.Node.Value)

}

func ParseDeployment(value string) (*cluster.Deployment, error) {
	deployment := cluster.Deployment{}
	bytes := []byte(value)
	if err := json.Unmarshal(bytes, &deployment); err != nil {
		return &cluster.Deployment{}, err
	}

	return &deployment, nil
}

func (registry *DeploymentRegistry) ListDeployments(namespace string) ([]cluster.Deployment, error) {
	keyName := fmt.Sprintf("/deployment/%v", namespace)

	resp, err := registry.etcdApi.Get(context.Background(), keyName, &client.GetOptions{Recursive: true})
	if err != nil {
		return []cluster.Deployment{}, err
	}

	result := []cluster.Deployment{}
	for _, node := range resp.Node.Nodes {
		deployment, err := ParseDeployment(node.Value)

		if err != nil {
			log.Println("Can't parse deployment descriptor: "+err.Error(), node.Value)
		} else {
			result = append(result, *deployment)
		}
	}

	return result, nil
}

func (registry *DeploymentRegistry) DeleteDeployment(namespace, id string) error {
	keyName := fmt.Sprintf("/deployment/%v/%v", namespace, id)

	_, err := registry.etcdApi.Delete(context.Background(), keyName, &client.DeleteOptions{Dir: false})
	return err
}
