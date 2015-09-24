package proxies

import (
	"testing"
	"github.com/coreos/etcd/client"
	"log"
	"os"
	"golang.org/x/net/context"
	"encoding/json"
)

var kAPI client.KeysAPI

func TestMain(m *testing.M) {
	c := createEtcdClient()
	kAPI = client.NewKeysAPI(c)

	_,err := kAPI.Delete(context.Background(), "/proxy", &client.DeleteOptions{Recursive:true, Dir:true})

	if err != nil {
		log.Println("Did not delete /proxy dir")
	}

	os.Exit(m.Run())
}

func createProxyConfigurator() *ProxyConfigurator{
	c := createEtcdClient()
	return NewProxyConfigurator(c)

}

func TestCreateProxyConfigurator(t *testing.T) {
	if createProxyConfigurator() == nil {
		t.FailNow()
	}
}

func TestAddBackendServer_newBackend(t *testing.T) {
	pc := createProxyConfigurator()

	if err := pc.AddBackendServer("testbackend", "127.0.0.1", 8080); err != nil {
		t.Error(err)
	}

	resp, err := kAPI.Get(context.Background(), "/proxy/backends/testbackend/127.0.0.1", nil)
	if err != nil {
		t.Error(err)
	}


	if resp == nil {
		t.Error("Key not found")
	}

	value := BackendServer{}

	if err = json.Unmarshal([]byte(resp.Node.Value), &value); err != nil {
		t.Error(err)
	}

	if value.Port != 8080 {
		t.Error("Incorrect port set for backend")
	}

	if value.IPAddress != "127.0.0.1" {
		t.Error("Incorrect ip set for backend")
	}
}

func TestAddBackendServer_existingBackend(t *testing.T) {
	pc := createProxyConfigurator()

	if err := pc.AddBackendServer("testbackend", "127.0.0.1", 8080); err != nil {
		t.Error(err)
	}

	if err := pc.AddBackendServer("testbackend", "127.0.0.2", 8181); err != nil {
		t.Error(err)
	}

	resp, err := kAPI.Get(context.Background(), "/proxy/backends/testbackend", nil)
	if err != nil {
		t.Error(err)
	}

	if resp.Node.Nodes.Len() != 2 {
		t.Error("Incorrect number of backend servers registered")
	}
}

func TestDeleteDeployment(t *testing.T) {
	pc := createProxyConfigurator()
	if err := pc.AddBackendServer("testbackend", "127.0.0.1", 8080); err != nil {
		t.Error(err)
	}

	pc.DeleteDeployment("testbackend")

	resp, _ := kAPI.Get(context.Background(), "/proxy/backends/testbackend", nil)

	if resp != nil {
		t.Error("Backend not deleted")
	}
}

func TestDeleteDeployment_NotExistingShouldntFail(t *testing.T) {
	pc := createProxyConfigurator()

	pc.DeleteDeployment("testbackend")
}

func TestCreateFrontend(t *testing.T) {
	pc := createProxyConfigurator()

	frontend := Frontend{
		Hostname: "myhostname.com",
		Type: "http",
		BackendId: "testbackend",
	}

	key, err := pc.CreateFrontEnd(&frontend)
	if err != nil {
		t.Error(err)
	}

	if key != "/proxy/frontends/myhostname.com" {
		t.Errorf("Incorrect hostname: %v", key)
	}

}

func TestCreateFrontend_ExistingShouldNotBeOverwritten(t *testing.T) {
	pc := createProxyConfigurator()

	frontend := Frontend{
		Hostname: "myhostname.com",
		Type: "http",
		BackendId: "testbackend",
	}

	pc.CreateFrontEnd(&frontend)

	frontend = Frontend{
		Hostname: "myhostname.com",
		Type: "http",
		BackendId: "testbackend2",
	}

	key, err := pc.CreateFrontEnd(&frontend)
	if err != nil {
		t.Error(err)
	}

	resp, err := kAPI.Get(context.Background(), key, nil)
	value := Frontend{}
	if err := json.Unmarshal([]byte(resp.Node.Value), &value); err != nil {
		t.Error(err)
	}

	if value.BackendId != "testbackend" {
		t.Error("Creating a frontend should not affect existing confguration")
	}
}

func TestSwitchBackend(t *testing.T) {
	pc := createProxyConfigurator()

	frontend := Frontend{
		Hostname: "myhostname.com",
		Type: "http",
		BackendId: "testbackend",
	}

	key, _ := pc.CreateFrontEnd(&frontend)
	if err := pc.SwitchBackend("myhostname.com", "mynewbackend"); err != nil {
		t.Error(err)
	}

	resp, _ := kAPI.Get(context.Background(), key, nil)

	value := Frontend{}

	if err := json.Unmarshal([]byte(resp.Node.Value), &value); err != nil {
		t.Error(err)
	}

	if value.BackendId != "mynewbackend" {
		t.Errorf("Incorrect backend: %v", value.BackendId)
	}
}

func TestBackendServer(t *testing.T) {
	pc := createProxyConfigurator()

	if err := pc.AddBackendServer("testbackend", "127.0.0.1", 8080); err != nil {
		t.Error(err)
	}

	if err := pc.AddBackendServer("testbackend", "127.0.0.2", 8181); err != nil {
		t.Error(err)
	}

	resp, _ := kAPI.Get(context.Background(), "/proxy/backends/testbackend", nil)

	if resp.Node.Nodes.Len() != 2 {
		t.Error("Incorrect number of backend servers registered")
	}

	 pc.DeleteBackendServer("testbackend", "127.0.0.1")

	resp,_ = kAPI.Get(context.Background(), "/proxy/backends/testbackend", nil)

	if resp.Node.Nodes.Len() != 1 {
		t.Error("Backend not deleted")
	}

}

func createEtcdClient() client.Client{
	cfg := client.Config{
		Endpoints: []string{"http://127.0.0.1:2379"},
	}

	c, err := client.New(cfg)
	if err != nil {
		log.Fatal("Couldn't connect to etcd")
	}

	return c
}

