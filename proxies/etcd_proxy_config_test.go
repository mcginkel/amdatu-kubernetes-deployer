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
	"github.com/coreos/etcd/client"
	"golang.org/x/net/context"
	"log"
	"os"
	"testing"
	"net/http/httptest"
	"net/http"
	"fmt"
	"time"
)

var kAPI client.KeysAPI

func TestMain(m *testing.M) {
	c := createEtcdClient()
	kAPI = client.NewKeysAPI(c)

	_, err := kAPI.Delete(context.Background(), "/proxy", &client.DeleteOptions{Recursive: true, Dir: true})

	if err != nil {
		log.Println("Did not delete /proxy dir")
	}

	os.Exit(m.Run())
}

func createProxyConfigurator(restUrl string) *ProxyConfigurator {
	c := createEtcdClient()
	return NewProxyConfigurator(c, restUrl, 2)

}

func TestCreateProxyConfigurator(t *testing.T) {
	if createProxyConfigurator("") == nil {
		t.FailNow()
	}
}

func TestAddBackendServer_newBackend(t *testing.T) {
	pc := createProxyConfigurator("")

	if err := pc.AddBackendServer("testbackend", "127.0.0.1", 8080, false); err != nil {
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
	pc := createProxyConfigurator("")

	if err := pc.AddBackendServer("testbackend", "127.0.0.1", 8080, false); err != nil {
		t.Error(err)
	}

	if err := pc.AddBackendServer("testbackend", "127.0.0.2", 8181, false); err != nil {
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
	pc := createProxyConfigurator("")
	if err := pc.AddBackendServer("testbackend", "127.0.0.1", 8080, false); err != nil {
		t.Error(err)
	}

	pc.DeleteDeployment("testbackend")

	resp, _ := kAPI.Get(context.Background(), "/proxy/backends/testbackend", nil)

	if resp != nil {
		t.Error("Backend not deleted")
	}
}

func TestDeleteDeployment_NotExistingShouldntFail(t *testing.T) {
	pc := createProxyConfigurator("")

	pc.DeleteDeployment("testbackend")
}

func TestCreateFrontend(t *testing.T) {
	pc := createProxyConfigurator("")

	frontend := Frontend{
		Hostname:  "myhostname.com",
		Type:      "http",
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
	pc := createProxyConfigurator("")

	frontend := Frontend{
		Hostname:  "myhostname.com",
		Type:      "http",
		BackendId: "testbackend",
	}

	pc.CreateFrontEnd(&frontend)

	frontend = Frontend{
		Hostname:  "myhostname.com",
		Type:      "http",
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
	pc := createProxyConfigurator("")

	frontend := Frontend{
		Hostname:  "myhostname.com",
		Type:      "http",
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
	pc := createProxyConfigurator("")

	if err := pc.AddBackendServer("testbackend", "127.0.0.1", 8080, false); err != nil {
		t.Error(err)
	}

	if err := pc.AddBackendServer("testbackend", "127.0.0.2", 8181, false); err != nil {
		t.Error(err)
	}

	resp, _ := kAPI.Get(context.Background(), "/proxy/backends/testbackend", nil)

	if resp.Node.Nodes.Len() != 2 {
		t.Error("Incorrect number of backend servers registered")
	}

	pc.DeleteBackendServer("testbackend", "127.0.0.1")

	resp, _ = kAPI.Get(context.Background(), "/proxy/backends/testbackend", nil)

	if resp.Node.Nodes.Len() != 1 {
		t.Error("Backend not deleted")
	}

}

func TestFrontendExistsForBackend_NotExisting(t *testing.T) {
	pc := createProxyConfigurator("")

	exists := pc.FrontendExistsForDeployment("somebackend")

	if exists {
		t.Fail()
	}
}

func TestFrontendExistsForBackend_Existing(t *testing.T) {
	pc := createProxyConfigurator("")

	kAPI.Delete(context.Background(), "/proxy", &client.DeleteOptions{Recursive: true, Dir: true})
	frontend := Frontend{
		Hostname:  "myhostname.com",
		Type:      "http",
		BackendId: "testbackend",
	}

	pc.CreateFrontEnd(&frontend)

	exists := pc.FrontendExistsForDeployment("testbackend")

	if !exists {
		t.Fail()
	}
}

func TestWaitForBackend_DOWN(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "DOWN")
	}))

	defer ts.Close()

	pc := createProxyConfigurator(ts.URL)

	successChan := make(chan bool)

	go runWaitForBackend(pc, successChan)

	select {
	case result := <-successChan:
		if result {
			t.Error("Backend available but shouldn't")
		}
		return
	case <-time.After(time.Second * 5):
		t.Error("Test timed out")
		return
	}

}

func TestWaitForBackend_UP(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "UP")
	}))

	defer ts.Close()

	pc := createProxyConfigurator(ts.URL)

	successChan := make(chan bool)

	go runWaitForBackend(pc, successChan)

	select {
	case result := <-successChan:
		if !result {
			t.Error("Backend did not report available")
		}
		return
	case <-time.After(time.Second * 5):
		t.Error("Test timed out")
		return
	}

}

func runWaitForBackend(pc *ProxyConfigurator, successChan chan bool) {
	r := pc.WaitForBackend("mybackend")

	successChan <-r
	return
}

func createEtcdClient() client.Client {
	cfg := client.Config{
		Endpoints: []string{"http://192.168.64.3:2379"},
	}

	c, err := client.New(cfg)
	if err != nil {
		log.Fatal("Couldn't connect to etcd")
	}

	return c
}
