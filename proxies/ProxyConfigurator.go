package proxies
import (
	"github.com/coreos/etcd/client"
	"log"
	"golang.org/x/net/context"
	"fmt"
	"encoding/json"
)

type ProxyConfigurator struct {
	etcdClient client.Client
}

type BackendServer struct {
	IPAddress string
	Port int
}

type Frontend struct {
	Hostname string
	Type string
	BackendId string
}

func NewProxyConfigurator(etcdClient client.Client) *ProxyConfigurator {
	return &ProxyConfigurator{etcdClient}
}

func (proxyConfigurator *ProxyConfigurator) AddBackendServer(deploymentName string, ip string, port int) error {
	kAPI := client.NewKeysAPI(proxyConfigurator.etcdClient)
	if err := prepareBaseConfig(kAPI); err != nil {
		return err
	}

	value := BackendServer{
		IPAddress: ip,
		Port: port,
	}

	bytes, err := json.Marshal(value)
	if err != nil {
		return err
	}

	if _,err := kAPI.Set(context.Background(), fmt.Sprintf("/proxy/backends/%v/%v", deploymentName, ip), string(bytes), nil); err != nil {
		return err
	}


	return nil
}

func (proxyConfigurator *ProxyConfigurator) DeleteDeployment(deploymentName string) {
	kAPI := client.NewKeysAPI(proxyConfigurator.etcdClient)
	keyName := fmt.Sprintf("/proxy/backends/%v", deploymentName)
	if _,err := kAPI.Delete(context.Background(), keyName, &client.DeleteOptions{Recursive:true}); err != nil {
		log.Printf("Key %v not found, nothing deleted", keyName)
	}
}

func (proxyConfigurator *ProxyConfigurator) CreateFrontEnd(frontend *Frontend) (string,error) {
	kAPI := client.NewKeysAPI(proxyConfigurator.etcdClient)
	if err := prepareBaseConfig(kAPI); err != nil {
		return "",err
	}

	bytes, err := json.Marshal(frontend)
	if err != nil {
		return "",err
	}

	key := fmt.Sprintf("/proxy/frontends/%v", frontend.Hostname)
	if _,err := kAPI.Set(context.Background(), key, string(bytes), nil); err != nil {
		return "", err
	}

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

	if _,err := kAPI.Set(context.Background(), key, string(bytes), nil); err != nil {
		return err
	}

	return nil
}

func (proxyConfigurator *ProxyConfigurator) DeleteBackendServer(deploymentName string, ip string) {
	kAPI := client.NewKeysAPI(proxyConfigurator.etcdClient)
	keyName := fmt.Sprintf("/proxy/backends/%v/%v", deploymentName, ip)
	if _,err := kAPI.Delete(context.Background(), keyName, nil); err != nil {
		log.Printf("Key %v not found, nothing deleted", keyName)
	}
}

func prepareBaseConfig(kAPI client.KeysAPI) error{
	_,err := kAPI.Get(context.Background(), "/proxy", nil)

	if err != nil {
		log.Println("/proxy doesn't exists, creating")

		if _,err = kAPI.Set(context.Background(), "/proxy", "", &client.SetOptions{Dir: true}); err != nil {
			return err
		}


	}

	return nil
}