package proxies

import (
	"com.amdatu.rti.deployment/Godeps/_workspace/src/github.com/coreos/etcd/client"
	"com.amdatu.rti.deployment/Godeps/_workspace/src/golang.org/x/net/context"
	"encoding/json"
	"fmt"
	"log"
)

type ProxyConfigurator struct {
	etcdClient client.Client
}

type BackendServer struct {
	IPAddress string
	Port      int
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
	kAPI := client.NewKeysAPI(proxyConfigurator.etcdClient)
	result, err := kAPI.Get(context.Background(), "/proxy/frontends", &client.GetOptions{})
	if err != nil {
		fmt.Println("Error listing frontends, now assuming the frontend doesn't exist")
		return false
	}

	for _, entry := range result.Node.Nodes {
		value := Frontend{}
		json.Unmarshal([]byte(entry.Value), &value)

		if value.BackendId == deploymentName {
			return true
		}
	}

	return false
}

func (proxyConfigurator *ProxyConfigurator) AddBackendServer(deploymentName string, ip string, port int) error {
	kAPI := client.NewKeysAPI(proxyConfigurator.etcdClient)
	if err := prepareBaseConfig(kAPI); err != nil {
		return err
	}

	value := BackendServer{
		IPAddress: ip,
		Port:      port,
	}

	bytes, err := json.Marshal(value)
	if err != nil {
		return err
	}

	if _, err := kAPI.Set(context.Background(), fmt.Sprintf("/proxy/backends/%v/%v", deploymentName, ip), string(bytes), nil); err != nil {
		return err
	}

	return nil
}

func (proxyConfigurator *ProxyConfigurator) DeleteDeployment(deploymentName string) {
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
			return err
		}

	}

	return nil
}
