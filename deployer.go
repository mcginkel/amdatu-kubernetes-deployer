package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"

	"crypto/tls"

	"net"
	"time"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/deployments"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/descriptors"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/etcdregistry"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/helper"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/k8s"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/migration"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/proxies"
	etcd "github.com/coreos/etcd/client"
	"github.com/gorilla/mux"
)

var kubernetesurl, etcdUrl, port, kubernetesUsername, kubernetesPassword string
var healthTimeout int
var proxyReloadSleep int
var skipServerCertValidation bool
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
	flag.IntVar(&healthTimeout, "timeout", 60, "Timeout in seconds for health checks")
	flag.IntVar(&proxyReloadSleep, "proxysleep", 20, "Seconds to wait for proxy to reload config")
	flag.BoolVar(&skipServerCertValidation, "skipServerCertValidation", false, "Skip server certificate validation")

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

	if skipServerCertValidation {
		var tlsConfig *tls.Config = &tls.Config{
			InsecureSkipVerify: true,
		}

		// copied from etcd client, only added tls config
		var transport etcd.CancelableTransport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			Dial: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).Dial,
			TLSHandshakeTimeout: 10 * time.Second,
			TLSClientConfig:     tlsConfig,
		}

		etcdCfg.Transport = transport
	}

	etcdClient, err := etcd.New(etcdCfg)
	if err != nil {
		log.Fatalf("Could not initialize etcd client! %v", err.Error())
	}

	etcdApi := etcd.NewKeysAPI(etcdClient)
	registry = etcdregistry.NewEtcdRegistry(etcdApi)

	k8sConfig := k8s.K8sConfig{
		ApiServerUrl: kubernetesurl,
	}
	k8sClient, err := k8s.New(k8sConfig)
	if err != nil {
		log.Fatalf("Could not initialize k8s client! %v", err.Error())
	}

	ingressConfigurator := proxies.NewIngressConfigurator(k8sClient, proxyReloadSleep, healthTimeout)

	mutexes := map[string]*sync.Mutex{}

	deployerConfig := helper.DeployerConfig{
		HealthTimeout:       healthTimeout,
		K8sClient:           k8sClient,
		EtcdRegistry:        registry,
		IngressConfigurator: ingressConfigurator,
		Mutexes:             mutexes,
	}

	if err := migration.Migrate(etcdApi, deployerConfig); err != nil {
		log.Fatalf("Error during migration: %v", err.Error())
	}

	descriptorHandlers = descriptors.NewDescriptorHandlers(registry)
	deploymentHandlers = deployments.NewDeploymentHandlers(deployerConfig)
}

func main() {

	r := mux.NewRouter()

	r.HandleFunc("/descriptors/", descriptorHandlers.CreateDescriptorHandler).Methods("POST")
	r.HandleFunc("/descriptors/", descriptorHandlers.ListDescriptorsHandler).Methods("GET")
	r.HandleFunc("/descriptors/{id}/", descriptorHandlers.GetDescriptorHandler).Methods("GET")
	r.HandleFunc("/descriptors/{id}/", descriptorHandlers.UpdateDescriptorHandler).Methods("PUT")
	r.HandleFunc("/descriptors/{id}/", descriptorHandlers.DeleteDescriptorHandler).Methods("DELETE")
	r.HandleFunc("/descriptors/validate", descriptorHandlers.DoValidationHandler).Methods("POST")

	r.HandleFunc("/deployments/", deploymentHandlers.CreateDeploymentHandler).Methods("POST")
	r.HandleFunc("/deployments/", deploymentHandlers.ListDeploymentsHandler).Methods("GET")
	r.HandleFunc("/deployments/", deploymentHandlers.DeleteDeploymentsHandler).Methods("DELETE")
	r.HandleFunc("/deployments/{id}/", deploymentHandlers.GetDeploymentHandler).Methods("GET")
	r.HandleFunc("/deployments/{id}/healthcheckdata", deploymentHandlers.GetHealthcheckDataHandler).Methods("GET")
	r.HandleFunc("/deployments/{id}/logs", deploymentHandlers.GetLogsHandler).Methods("GET")
	r.HandleFunc("/deployments/{id}/", deploymentHandlers.UpdateDeploymentHandler).Methods("PUT")
	r.HandleFunc("/deployments/{id}/", deploymentHandlers.DeleteDeploymentHandler).Methods("DELETE")

	r.HandleFunc("/stream/deployments/{id}/logs", deploymentHandlers.StreamLogsHandler)

	fmt.Printf("Deployer starting and listening on port %v\n", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal(err)
	}

}
