package cluster
import (
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"encoding/json"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/coreos/go-etcd/etcd"
	"log"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"errors"
	"fmt"
	"com.amdatu.rti.deployment/healthcheck"
	"time"
	"net/http"
	"io"
)

type Deployment struct {
	DeploymentType string `json:"deploymentType,omitempty"`
	NewVersion string `json:"newVersion,omitempty"`
	AppName string `json:"appName,omitempty"`
	Replicas int `json:"replicas,omitempty"`
	VulcanFrontend string `json:"vulcanFrontend,omitempty"`
	PodSpec api.PodSpec
	UseHealthCheck bool `json:"useHealthCheck,omitempty"`
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
	EtcdClient *etcd.Client
	Logger *Logger
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

func NewDeployer(kubernetesUrl string, etcdUrl string, deployment Deployment, logger *Logger) *Deployer{

	config := client.Config{Host: kubernetesUrl}
	c, err := client.New(&config)

	if err != nil {
		log.Panic("Error creating Kuberentes client", err)
	}

	logger.Printf("Connected to Kubernetes API server on %v\n", kubernetesUrl)
	logger.Printf("Kubernetes version %v\n", c.APIVersion())

	machines := []string{etcdUrl}
	etcdClient := etcd.NewClient(machines)

	return &Deployer{kubernetesUrl, deployment, etcdUrl, c, etcdClient, logger}

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
		} ,
		Replicas: deployer.Deployment.Replicas,
		Template: &api.PodTemplateSpec {
			ObjectMeta: api.ObjectMeta{
				Labels: map[string]string {
					"name": rcName,
					"version": deployer.Deployment.NewVersion,
				},
			},
			Spec: deployer.Deployment.PodSpec,
		},
	}

	deployer.Logger.Println("Creating Replication Controller")
	return deployer.K8client.ReplicationControllers(api.NamespaceDefault).Create(ctrl)

}

func (deployer *Deployer)FindCurrentRc() ([]api.ReplicationController, error) {
	result := make([]api.ReplicationController, 1, 10)

	rcLabelSelector := labels.Set{"app": deployer.Deployment.AppName}.AsSelector()
	replicationControllers,_ := deployer.K8client.ReplicationControllers(api.NamespaceDefault).List(rcLabelSelector)

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

func (deployer *Deployer) CleaupOldDeployments() {
	controllers, err := deployer.FindCurrentRc()

	if err != nil {
		deployer.Logger.Println("Did not find a old Replication Controller to remove")
		return
	}

	for _,rc := range controllers {
		deployer.deleteRc(rc)
		deployer.deleteVulcanBackend(rc)
	}

}

func (deployer *Deployer) deleteRc(rc api.ReplicationController) {
	deployer.Logger.Printf("Deleting RC %v", rc.Name)

	rc.Spec.Replicas = 0
	deployer.K8client.ReplicationControllers(api.NamespaceDefault).Update(rc)
	time.Sleep(20 * time.Second)

	deployer.K8client.ReplicationControllers(api.NamespaceDefault).Delete(rc.Name)
}

func (deployer *Deployer) deleteVulcanBackend(rc api.ReplicationController) {
	backendName := fmt.Sprintf("%v-%v", rc.Labels["name"], rc.Labels["version"])
	keyName := fmt.Sprintf("/vulcan/backends/%v", backendName)

	deployer.Logger.Printf("Deleting Vulcan backend %v", keyName)
	deployer.EtcdClient.Delete(keyName, true)
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