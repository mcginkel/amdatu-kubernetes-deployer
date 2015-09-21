package cluster
import (
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"encoding/json"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"log"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"errors"
	"fmt"
	"com.amdatu.rti.deployment/healthcheck"
	"time"
	"net/http"
	"io"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/fields"
	"com.amdatu.rti.deployment/proxies"
	etcdclient "github.com/coreos/etcd/client"
)

type Deployment struct {
	DeploymentType string `json:"deploymentType,omitempty"`
	NewVersion string `json:"newVersion,omitempty"`
	AppName string `json:"appName,omitempty"`
	Replicas int `json:"replicas,omitempty"`
	VulcanFrontend string `json:"vulcanFrontend,omitempty"`
	PodSpec api.PodSpec
	UseHealthCheck bool `json:"useHealthCheck,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Email string `json:"email,omitempty`
	Password string `json:"password,omitempty`
}

func (deployment *Deployment) String() string {
	b, err := json.MarshalIndent(deployment,"", "    ")

	if err != nil {
		return "Error writing deployment to JSON"
	}

	return string(b)
}

type Deployer struct {
	KubernetesUrl string
	Deployment Deployment
	EtcdUrl string
	K8client *client.Client
	Logger *Logger
	ProxyConfigurator *proxies.ProxyConfigurator
}

type Logger struct {
	RespWriter http.ResponseWriter
}

func (logger *Logger) Println(v ...interface{}) {
	msg := fmt.Sprintln(v...)
	log.Println(msg)
	io.WriteString(logger.RespWriter, msg)
}

func (logger *Logger) Printf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	log.Printf(msg)
	io.WriteString(logger.RespWriter, msg)
}

func NewDeployer(kubernetesUrl string, kubernetesUsername string, kubernetesPassword string, etcdUrl string, deployment Deployment, logger *Logger) *Deployer{

	config := client.Config{Host: kubernetesUrl, Version: "v1", Username: kubernetesUsername, Password: kubernetesPassword, Insecure:true }
	c, err := client.New(&config)

	if err != nil {
		log.Panic("Error creating Kuberentes client", err)
	}

	logger.Printf("Connected to Kubernetes API server on %v\n", kubernetesUrl)
	logger.Printf("Kubernetes version %v\n", c.APIVersion())

	cfg := etcdclient.Config{
		Endpoints: []string{"http://127.0.0.1:2379"},
	}

	etcdClient, err := etcdclient.New(cfg)
	if err != nil {
		log.Fatal("Couldn't connect to etcd")
	}

	return &Deployer{kubernetesUrl, deployment, etcdUrl, c, logger, proxies.NewProxyConfigurator(etcdClient)}

}

func (deployer *Deployer) CreateRcName() string {
	return deployer.Deployment.AppName + "-" + deployer.Deployment.NewVersion
}

func (deployer *Deployer) CreateReplicationController() (*api.ReplicationController, error) {

	ctrl := new(api.ReplicationController)
	rcName := deployer.CreateRcName()
	ctrl.Name = rcName

	labels := make(map[string]string)
	labels["name"] = rcName
	labels["version"] = deployer.Deployment.NewVersion
	labels["app"] = deployer.Deployment.AppName

	ctrl.Labels = labels

	ctrl.Spec = api.ReplicationControllerSpec {
		Selector: map[string]string{
			"name": rcName,
			"version": deployer.Deployment.NewVersion,
			"app": deployer.Deployment.AppName,
		},
		Replicas: deployer.Deployment.Replicas,
		Template: &api.PodTemplateSpec {
			ObjectMeta: api.ObjectMeta{
				Labels: map[string]string {
					"name": rcName,
					"version": deployer.Deployment.NewVersion,
					"app": deployer.Deployment.AppName,
				},
			},
			Spec: deployer.Deployment.PodSpec,
		},
	}

	deployer.Logger.Println("Creating Replication Controller")
	var result, err = deployer.K8client.ReplicationControllers(deployer.Deployment.Namespace).Create(ctrl)
	if err != nil {
		deployer.Logger.Println("Error while creating replication controller")
		deployer.Logger.Println(err)
	}

	deployer.Logger.Printf("Replication Controller %v created\n", result.ObjectMeta.Name)

	return result,err

}

func (deployer *Deployer) CreateService() (*api.Service, error) {
	srv := new(api.Service)
	srv.Name = deployer.CreateRcName()

	selector := make(map[string]string)
	selector["name"] = deployer.CreateRcName()
	selector["version"] = deployer.Deployment.NewVersion
	selector["app"] = deployer.Deployment.AppName

	srv.Labels = selector

	srv.Spec = api.ServiceSpec{
		Selector: selector,
		Ports: []api.ServicePort {
			api.ServicePort{
				//TargetPort: util.NewIntOrStringFromString("None"),
				Port: deployer.Deployment.PodSpec.Containers[0].Ports[0].ContainerPort,
			},
		},
	}

	deployer.Logger.Println("Creating Service")

	return deployer.K8client.Services(deployer.Deployment.Namespace).Create(srv)
}

func (deployer *Deployer)FindCurrentRc() ([]api.ReplicationController, error) {
	result := make([]api.ReplicationController, 1, 10)

	rcLabelSelector := labels.Set{"app": deployer.Deployment.AppName}.AsSelector()
	replicationControllers,_ := deployer.K8client.ReplicationControllers(deployer.Deployment.Namespace).List(rcLabelSelector)

	for _,rc := range replicationControllers.Items {
		if(rc.Labels["version"] != deployer.Deployment.NewVersion) {

			result = append(result, rc)
		}
	}

	if len(result) == 0 {
		return result, errors.New("No active Replica Controller found")
	} else {
		return result, nil
	}
}

func (deployer *Deployer)FindCurrentPods(allowSameVersion bool) ([]api.Pod, error) {
	result := make([]api.Pod, 0, 10)

	rcLabelSelector := labels.Set{"app": deployer.Deployment.AppName}.AsSelector()
	pods,_ := deployer.K8client.Pods(deployer.Deployment.Namespace).List(rcLabelSelector, fields.Everything())

	for _,rc := range pods.Items {
		if(allowSameVersion || rc.Labels["version"] != deployer.Deployment.NewVersion) {

			result = append(result, rc)
		}
	}

	if len(result) == 0 {
		return result, errors.New("No active Pods found")
	} else {
		return result, nil
	}
}

func (deployer *Deployer)FindCurrentService() ([]api.Service, error) {
	result := make([]api.Service, 1, 10)

	rcLabelSelector := labels.Set{"app": deployer.Deployment.AppName}.AsSelector()
	services, _ := deployer.K8client.Services(deployer.Deployment.Namespace).List(rcLabelSelector)

	for _, service := range services.Items {
		if (service.Labels["version"] != deployer.Deployment.NewVersion) {

			result = append(result, service)
		}
	}

	if len(result) == 0 {
		return result, errors.New("No active Service found")
	} else {
		return result, nil
	}
}

func (deployer *Deployer) CleaupOldDeployments() {
	controllers, err := deployer.FindCurrentRc()

	if err != nil {
		deployer.Logger.Println("Did not find a old Replication Controller to remove")
		return
	}

	for _,rc := range controllers {
		if rc.Name != "" {
			deployer.deleteRc(rc)
			deployer.ProxyConfigurator.DeleteDeployment(rc.Name)
		}
	}

	log.Println("Looking for old pods...")
	pods, err := deployer.FindCurrentPods(false)

	if err != nil {
		deployer.Logger.Println("Did not find old pods to remove")
	}

	for _, pod := range pods {
		if pod.Name != "" {
			deployer.DeletePod(pod)
		}
	}

	log.Println("Looking for services...")
	services, err := deployer.FindCurrentService()

	if err != nil {
		deployer.Logger.Println("Did not find a old Replication Controller to remove")
		return
	}

	for _, service := range services {
		if service.Name != "" {
			deployer.deleteService(service)
		}
	}
}

func (deployer *Deployer) deleteRc(rc api.ReplicationController) {
	deployer.Logger.Printf("Deleting RC %v", rc.Name)

	deployer.K8client.ReplicationControllers(deployer.Deployment.Namespace).Delete(rc.Name)
}

func (deployer *Deployer) DeletePod(pod api.Pod) {
	deployer.Logger.Printf("Deleting Pod %v", pod.Name)

	deployer.K8client.Pods(deployer.Deployment.Namespace).Delete(pod.Name, &api.DeleteOptions{})
}

func (deployer *Deployer) deleteService(service api.Service) {
	deployer.Logger.Printf("Deleting Service %v", service.Name)
	deployer.K8client.Services(deployer.Deployment.Namespace).Delete(service.Name)
}

func (deployer *Deployer) CountRunningPods(pods []api.Pod) int {
	nrOfRunning := 0

	for _,pod := range pods {
		if pod.Status.Phase == "Running" {
			nrOfRunning++
		}
	}

	return nrOfRunning
}

func (deployer *Deployer) CheckPodHealth(pod *api.Pod) error {
	if deployer.Deployment.UseHealthCheck {
		port := pod.Spec.Containers[0].Ports[0].ContainerPort
		host := pod.Status.PodIP

		healthy := healthcheck.WaitForPodStarted(fmt.Sprintf("http://%v:%v/health", host,port), time.Minute * 5)
		if !healthy {
			return errors.New("Pod didn't get healthy")
		}
	}

	return nil
}