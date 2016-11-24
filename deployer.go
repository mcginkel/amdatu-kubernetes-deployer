package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/deployments"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/descriptors"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/etcdregistry"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/helper"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/migration"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/proxies"
	etcd "github.com/coreos/etcd/client"
	"github.com/gorilla/mux"
)

var kubernetesurl, etcdUrl, port, kubernetesUsername, kubernetesPassword, proxyRestUrl string
var healthTimeout int64
var proxyReloadSleep int
var registry *etcdregistry.EtcdRegistry
var descriptorHandlers *descriptors.DescriptorHandlers
var deploymentHandlers *deployments.DeploymentHandlers

type deploymentStatus struct {
	Success   bool   `json:"success"`
	Ts        string `json:"ts,omitempty"`
	Podstatus string `json:"podstatus,omitempty"`
}

func init() {
	flag.StringVar(&kubernetesurl, "kubernetes", "", "URL to the Kubernetes API server")
	flag.StringVar(&etcdUrl, "etcd", "", "Url to etcd")
	flag.StringVar(&port, "deployport", "8000", "Port to listen for deployments")
	flag.StringVar(&kubernetesUsername, "kubernetesusername", "noauth", "Username to authenticate against Kubernetes API server. Skip authentication when not set")
	flag.StringVar(&kubernetesPassword, "kubernetespassword", "noauth", "Username to authenticate against Kubernetes API server.")
	flag.Int64Var(&healthTimeout, "timeout", 60, "Timeout in seconds for health checks")
	flag.IntVar(&proxyReloadSleep, "proxysleep", 20, "Seconds to wait for proxy to reload config")
	flag.StringVar(&proxyRestUrl, "proxyrest", "", "Proxy REST url")

	exampleUsage := "Missing required argument %v. Example usage: ./deployer_linux_amd64 -kubernetes http://[kubernetes-api-url]:8080 -etcd http://[etcd-url]:2379 -deployport 8000"

	flag.Parse()

	if kubernetesurl == "" {
		log.Fatalf(exampleUsage, "kubernetes")
	}

	if etcdUrl == "" {
		log.Fatalf(exampleUsage, "etcd")
	}

	etcdCfg := etcd.Config{
		Endpoints: []string{etcdUrl},
	}

	etcdClient, err := etcd.New(etcdCfg)
	if err != nil {
		log.Fatalf("Could not initialize etcd client! %v", err.Error())
	}

	etcdApi := etcd.NewKeysAPI(etcdClient)
	registry = etcdregistry.NewEtcdRegistry(etcdApi)

	if err := migration.Migrate(etcdApi, registry); err != nil {
		log.Fatal(err)
	}

	descriptorHandlers = descriptors.NewDescriptorHandlers(registry)

	proxyConfigurator := proxies.NewProxyConfigurator(etcdApi, proxyRestUrl, proxyReloadSleep)

	mutexes := map[string]*sync.Mutex{}

	deployerConfig := helper.DeployerConfig{
		K8sUrl:            kubernetesurl,
		K8sUsername:       kubernetesUsername,
		K8sPassword:       kubernetesPassword,
		HealthTimeout:     healthTimeout,
		EtcdRegistry:      registry,
		ProxyConfigurator: proxyConfigurator,
		Mutexes:           mutexes,
	}
	deploymentHandlers = deployments.NewDeploymentHandlers(deployerConfig)
}

func main() {

	r := mux.NewRouter()

	r.HandleFunc("/descriptors/", descriptorHandlers.CreateDescriptorHandler).Methods("POST")
	r.HandleFunc("/descriptors/", descriptorHandlers.ListDescriptorsHandler).Methods("GET")
	r.HandleFunc("/descriptors/{id}", descriptorHandlers.GetDescriptorHandler).Methods("GET")
	r.HandleFunc("/descriptors/{id}", descriptorHandlers.UpdateDescriptorHandler).Methods("PUT")
	r.HandleFunc("/descriptors/{id}", descriptorHandlers.DeleteDescriptorHandler).Methods("DELETE")
	r.HandleFunc("/descriptors/validate", descriptorHandlers.DoValidationHandler).Methods("POST")

	r.HandleFunc("/deployments/", deploymentHandlers.CreateDeploymentHandler).Methods("POST")
	r.HandleFunc("/deployments/", deploymentHandlers.ListDeploymentsHandler).Methods("GET")
	r.HandleFunc("/deployments/{id}", deploymentHandlers.GetDeploymentHandler).Methods("GET")
	r.HandleFunc("/deployments/{id}/healthcheckdata", deploymentHandlers.GetHealthcheckDataHandler).Methods("GET")
	r.HandleFunc("/deployments/{id}/logs", deploymentHandlers.GetLogsHandler).Methods("GET")
	r.HandleFunc("/deployments/{id}", deploymentHandlers.UpdateDeploymentHandler).Methods("PUT")
	r.HandleFunc("/deployments/{id}", deploymentHandlers.DeleteDeploymentHandler).Methods("DELETE")

	r.HandleFunc("/stream/deployments/{id}/logs", deploymentHandlers.StreamLogsHandler)

	fmt.Printf("Deployer starting and listening on port %v\n", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal(err)
	}

}
