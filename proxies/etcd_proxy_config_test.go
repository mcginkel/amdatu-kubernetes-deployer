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
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	"github.com/coreos/etcd/client"
	"golang.org/x/net/context"
	"k8s.io/client-go/pkg/api/v1"
)

var kAPI client.KeysAPI

func TestMain(m *testing.M) {
	kAPI = createEtcdApi()

	_, err := kAPI.Delete(context.Background(), "/proxy", &client.DeleteOptions{Recursive: true, Dir: true})

	if err != nil {
		log.Println("Did not delete /proxy dir")
	}

	os.Exit(m.Run())
}

func createProxyConfigurator(restUrl string) *ProxyConfigurator {
	kAPI := createEtcdApi()
	return NewProxyConfigurator(kAPI, restUrl, 2)

}

func TestCreateProxyConfigurator(t *testing.T) {
	if createProxyConfigurator("") == nil {
		t.FailNow()
	}
}

func TestAddBackendServer_newBackend(t *testing.T) {
	pc := createProxyConfigurator("")

	desc := &types.Descriptor{}
	svc := &v1.Service{
		Spec: v1.ServiceSpec{
			ClusterIP: "127.0.0.1",
			Ports: []v1.ServicePort{
				{
					Port: 8080,
				},
			},
		},
	}

	if err := pc.addBackendServer("testbackend", desc, svc, logger.NewConsoleLogger()); err != nil {
		t.Error(err)
	}

	resp, err := kAPI.Get(context.Background(), "/proxy/backends/testbackend/127.0.0.1", nil)
	if err != nil {
		t.Error(err)
	}

	if resp == nil {
		t.Error("Key not found")
	}

	value := backendServer{}

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

	desc := &types.Descriptor{}
	svc1 := &v1.Service{
		Spec: v1.ServiceSpec{
			ClusterIP: "127.0.0.1",
			Ports: []v1.ServicePort{
				{
					Port: 8080,
				},
			},
		},
	}
	svc2 := &v1.Service{
		Spec: v1.ServiceSpec{
			ClusterIP: "127.0.0.2",
			Ports: []v1.ServicePort{
				{
					Port: 8181,
				},
			},
		},
	}

	if err := pc.addBackendServer("testbackend", desc, svc1, logger.NewConsoleLogger()); err != nil {
		t.Error(err)
	}

	if err := pc.addBackendServer("testbackend", desc, svc2, logger.NewConsoleLogger()); err != nil {
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
	desc := &types.Descriptor{}
	svc := &v1.Service{
		Spec: v1.ServiceSpec{
			ClusterIP: "127.0.0.1",
			Ports: []v1.ServicePort{
				{
					Port: 8080,
				},
			},
		},
	}

	if err := pc.addBackendServer("testbackend", desc, svc, logger.NewConsoleLogger()); err != nil {
		t.Error(err)
	}

	pc.DeleteBackend("testbackend", logger.NewConsoleLogger())

	resp, _ := kAPI.Get(context.Background(), "/proxy/backends/testbackend", nil)

	if resp != nil {
		t.Error("Backend not deleted")
	}
}

func TestDeleteDeployment_NotExistingShouldntFail(t *testing.T) {
	pc := createProxyConfigurator("")

	pc.DeleteBackend("testbackend", logger.NewConsoleLogger())
}

func TestCreateFrontend(t *testing.T) {
	pc := createProxyConfigurator("")

	frontend := frontend{
		Hostname:  "myhostname.com",
		Type:      "http",
		BackendId: "testbackend",
	}

	err := pc.createFrontEnd(&frontend, logger.NewConsoleLogger())
	if err != nil {
		t.Error(err)
	}

}

func TestCreateFrontend_ExistingShouldNotBeOverwritten(t *testing.T) {
	pc := createProxyConfigurator("")

	frontend := frontend{
		Hostname:  "myhostname.com",
		Type:      "http",
		BackendId: "testbackend",
	}

	pc.createFrontEnd(&frontend, logger.NewConsoleLogger())

	frontend = frontend{
		Hostname:  "myhostname.com",
		Type:      "http",
		BackendId: "testbackend2",
	}

	err := pc.createFrontEnd(&frontend, logger.NewConsoleLogger())
	if err != nil {
		t.Error(err)
	}

	resp, err := kAPI.Get(context.Background(), "/proxy/frontends/myhostname.com", nil)
	value := frontend{}
	if err := json.Unmarshal([]byte(resp.Node.Value), &value); err != nil {
		t.Error(err)
	}

	if value.BackendId != "testbackend" {
		t.Error("Creating a frontend should not affect existing confguration")
	}
}

func TestSwitchBackend(t *testing.T) {
	pc := createProxyConfigurator("")

	frontend := frontend{
		Hostname:  "myhostname.com",
		Type:      "http",
		BackendId: "testbackend",
	}

	pc.createFrontEnd(&frontend, logger.NewConsoleLogger())
	if err := pc.switchBackend("myhostname.com", "mynewbackend", logger.NewConsoleLogger()); err != nil {
		t.Error(err)
	}

	resp, _ := kAPI.Get(context.Background(), "/proxy/frontends/myhostname.com", nil)

	value := frontend{}

	if err := json.Unmarshal([]byte(resp.Node.Value), &value); err != nil {
		t.Error(err)
	}

	if value.BackendId != "mynewbackend" {
		t.Errorf("Incorrect backend: %v", value.BackendId)
	}
}

func TestWaitForBackend_DOWN(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `"DOWN"`)
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
		fmt.Fprintln(w, `"UP"`)
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

func TestPrefixHeaderValues(t *testing.T) {
	headers := []types.HttpHeader{{Header: "test", Value: "some string\\ with  spaces"}}
	prefixSpacesInHeaderValues(headers)
	if !(headers[0].Value == "some\\ string\\ with\\ \\ spaces") {
		t.Error("prefixSpacesInHeaderValues failed: " + headers[0].Value)
	}
}

func runWaitForBackend(pc *ProxyConfigurator, successChan chan bool) {
	err := pc.waitForBackend("mybackend", logger.NewConsoleLogger())

	if err != nil {
		successChan <- false
	} else {
		successChan <- true
	}
}

func createEtcdApi() client.KeysAPI {
	cfg := client.Config{
		Endpoints: []string{"http://localhost:2379"},
	}

	c, err := client.New(cfg)
	if err != nil {
		log.Fatal("Couldn't connect to etcd")
	}

	return client.NewKeysAPI(c)
}
