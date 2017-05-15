package migration

import (
	"errors"
	"path"
	"strings"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/etcdregistry"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/helper"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/k8s"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/proxies"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	etcdclient "github.com/coreos/etcd/client"
	"golang.org/x/net/context"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const MIGRATION_KEY = "deployer/ingressMigrationDone"

var etcdApi etcdclient.KeysAPI
var registry *etcdregistry.EtcdRegistry
var k8sClient *k8s.K8sClient
var ingressConfigurator *proxies.IngressConfigurator

func Migrate(newEtcdApi etcdclient.KeysAPI, config helper.DeployerConfig) error {

	etcdApi = newEtcdApi
	registry = config.EtcdRegistry
	k8sClient = config.K8sClient
	ingressConfigurator = config.IngressConfigurator

	myLogger := logger.NewConsoleLogger()

	// check if migration was done already
	_, err := etcdApi.Get(context.Background(), MIGRATION_KEY, nil)
	if err != nil {
		if strings.Contains(err.Error(), "Key not found") {
			myLogger.Println("Creating ingresses for active deployments...")
		} else {
			return errors.New("Could not read migration done marker: " + err.Error())
		}
	} else {
		myLogger.Println("Ingress migration already done")
		return nil
	}

	// migrate
	err = migrate(myLogger)
	if err != nil {
		return err
	}

	// mark migration as done
	_, err = etcdApi.Set(context.Background(), MIGRATION_KEY, "done", nil)
	if err != nil {
		myLogger.Printf("Error during marking migration as done: %v", err.Error())
		return err
	}

	return nil
}

func migrate(myLogger logger.Logger) error {

	// get namespaces
	namespaceNodes, err := etcdApi.Get(context.Background(), "deployer/deployments", nil)
	if err != nil {
		return errors.New("Could not read deployments for getting namespaces: " + err.Error())
	}

	for _, namespaceNode := range namespaceNodes.Node.Nodes {
		namespace := path.Base(namespaceNode.Key)

		myLogger.Printf("Namespace: %v", namespace)

		// get active deployments
		deployments, err := registry.GetDeployments(namespace)
		if err != nil {
			return errors.New("  Could not read deployments: " + err.Error())
		}
		for _, deployment := range deployments {
			if deployment.Status == types.DEPLOYMENTSTATUS_DEPLOYED {

				deploymentName := deployment.Descriptor.AppName + "-" + deployment.Version
				myLogger.Printf("  Deployment: %v", deploymentName)

				if len(deployment.Descriptor.Frontend) > 0 {

					// find service
					service, err := k8sClient.GetService(namespace, deploymentName)
					if statusError, isStatus := err.(*k8sErrors.StatusError); isStatus && statusError.Status().Reason == meta.StatusReasonNotFound {
						myLogger.Printf("    Could not find service for %v, skipping Ingress creation!", deploymentName)
						continue
					} else if err != nil {
						return errors.New("Could not get service: " + err.Error())
					}

					// create ingress
					deploymentLogger := logger.NewDeploymentLogger(deployment, registry, myLogger)
					deploymentLogger.Printf("    Creating Ingress during migration for %v", deploymentName)
					if err = ingressConfigurator.CreateOrUpdateProxy(deployment, service, deploymentLogger); err != nil {
						deploymentLogger.Printf("      Error during creation of Ingress for %v: %v", deploymentName, err.Error())
						return err
					}
					deploymentLogger.Printf("    Successfully created Ingress during migration for %v", deploymentName)
				} else {
					myLogger.Printf("    No frontend, skipping...")
				}
			}
		}
	}

	return nil

}
