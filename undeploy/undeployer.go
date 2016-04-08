package undeploy

import (
	"errors"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/cluster"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/deploymentregistry"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/proxies"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-go/api/v1"
	k8sclient "bitbucket.org/amdatulabs/amdatu-kubernetes-go/client"
	etcdclient "github.com/coreos/etcd/client"
)

type undeployer struct {
	namespace string
	appname   string
	registry  *deploymentregistry.DeploymentRegistry
	proxy     *proxies.ProxyConfigurator
	k8sclient k8sclient.Client
	logger    cluster.Logger
}

func NewUndeployer(namespace string, appname string, etcdUrl string,
	kubernetesUrl string, kubernetesUsername string, kubernetesPassword string,
	logger cluster.Logger) (*undeployer, error) {

	cfg := etcdclient.Config{
		Endpoints: []string{etcdUrl},
	}

	etcdClient, err := etcdclient.New(cfg)
	if err != nil {
		logger.Println("Error connecting to etcd: " + err.Error())
		return nil, err
	}

	registry := deploymentregistry.NewDeploymentRegistry(&etcdClient)

	proxy := proxies.NewProxyConfigurator(etcdClient)

	client := k8sclient.NewClient(kubernetesUrl, kubernetesUsername, kubernetesPassword)
	logger.Printf("Connected to Kubernetes API server on %v\n", kubernetesUrl)

	return &undeployer{namespace, appname, &registry, proxy, client, logger}, nil
}

func (undeployer *undeployer) Undeploy() error {

	undeployer.logger.Printf("Starting undeployment of application %v in namespace %v\n", undeployer.appname, undeployer.namespace)

	controllers, err := undeployer.getReplicationControllers()
	if err != nil {
		undeployer.logger.Printf("error getting controllers: %v\n", err.Error())
		return err
	}

	error := ""

	for _, controller := range controllers {
		backend := undeployer.namespace + "-" + controller.ObjectMeta.Name
		undeployer.deleteProxy(backend)
	}

	if err = undeployer.deleteServices(); err != nil {
		undeployer.logger.Printf("error deleting services: %v\n", err.Error())
		error += err.Error() + "\n"
	}

	for _, controller := range controllers {
		if undeployer.deleteController(controller); err != nil {
			undeployer.logger.Printf("error deleting controller: %v\n", err.Error())
			error += err.Error() + "\n"
		}
	}

	if err = undeployer.deleteDeploymentHistories(); err != nil {
		undeployer.logger.Printf("error deleting history: %v\n", err)
		error += err.Error() + "\n"
	}

	if len(error) > 0 {
		return errors.New(error)
	} else {
		return nil
	}
}

func (undeployer *undeployer) getReplicationControllers() ([]v1.ReplicationController, error) {
	undeployer.logger.Printf("Getting replication controllers\n")

	labels := map[string]string{"app": undeployer.appname}
	rcList, err := undeployer.k8sclient.ListReplicationControllersWithLabel(undeployer.namespace, labels)
	if err != nil {
		return []v1.ReplicationController{}, err
	}
	return rcList.Items, nil
}

func (undeployer *undeployer) deleteProxy(backend string) {
	undeployer.logger.Printf("Deleting proxy backend for %v\n", backend)
	undeployer.proxy.DeleteDeployment(backend)

	undeployer.logger.Printf("Deleting proxy frontend for %v\n", backend)
	undeployer.proxy.DeleteFrontendForDeployment(backend)
}

func (undeployer *undeployer) deleteServices() error {
	labels := map[string]string{"app": undeployer.appname}
	servicelist, err := undeployer.k8sclient.ListServicesWithLabel(undeployer.namespace, labels)
	if err != nil {
		return err
	}
	for _, service := range servicelist.Items {
		undeployer.logger.Printf("Deleting service %v\n", service.ObjectMeta.Name)
		err := undeployer.k8sclient.DeleteService(undeployer.namespace, service.ObjectMeta.Name)
		if err != nil {
			return err
		}
	}
	return nil
}

func (undeployer *undeployer) deleteController(controller v1.ReplicationController) error {
	undeployer.logger.Printf("Scaling down replication controller %v\n", controller.ObjectMeta.Name)
	replicas := int32(0)
	controller.Spec.Replicas = &replicas;
	_, err := undeployer.k8sclient.UpdateReplicationController(undeployer.namespace, &controller)
	if err != nil {
		return err
	}

	undeployer.logger.Printf("Deleting replication controller %v\n", controller.ObjectMeta.Name)
	err = undeployer.k8sclient.DeleteReplicationController(undeployer.namespace, controller.ObjectMeta.Name)
	if err != nil {
		return err
	}
	return nil
}

func (undeployer *undeployer) deleteDeploymentHistories() error {
	histories, err := undeployer.registry.ListDeploymentsWithAppname(undeployer.namespace, undeployer.appname)
	if err != nil {
		return err
	}
	for _, history := range histories {
		undeployer.logger.Printf("Deleting deployment history with id %v\n", history.Id)
		err = undeployer.registry.DeleteDeployment(undeployer.namespace, history.Id)
		if err != nil {
			return err
		}
	}
	return nil
}
