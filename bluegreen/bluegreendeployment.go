package bluegreen

/**
1) Create new RC, with a label name that includes the version.
2) Watch Kubernetes for Pods to become RUNNING
3) Switch backend in Vulcan
4) Remove old backend from Vulcan
5) Remove old RC from Kubernetes
 */

import (
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"log"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"encoding/json"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/fields"
	"time"
	"github.com/coreos/go-etcd/etcd"
	"fmt"
	"strings"
	"errors"
	"com.amdatu.rti.deployment/healthcheck"
)

type Deployment struct {
	NewVersion string `json:"newVersion,omitempty"`
	AppName string `json:"appName,omitempty"`
	Replicas int `json:"replicas,omitempty"`
	VulcanFrontend string `json:"vulcanFrontend,omitempty"`
	EtcdUrl string `json:"etcdurl,omitempty"`
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

type deployer struct {
	KubernetesUrl string
	Deployment Deployment
	k8client *client.Client
	etcdClient *etcd.Client
}

func NewDeployer(kubernetesUrl string, deployment Deployment) *deployer{

	config := client.Config{Host: kubernetesUrl}
	c, err := client.New(&config)

	if err != nil {
		log.Panic("Error creating Kuberentes client", err)
	}

	machines := []string{deployment.EtcdUrl}
	etcdClient := etcd.NewClient(machines)

	return &deployer{kubernetesUrl, deployment, c, etcdClient}

}

func (deployer *deployer) Deploy() error {
	if err := deployer.createReplicationController(); err != nil {
		return err
	}

	if err := deployer.switchVulcanBackend(); err != nil {
		return err
	}

	if err := deployer.prepareNewVulcanBackend(); err != nil {
		return err
	}

	deployer.cleaupOldDeployments()

	return nil
}

func (deployer *deployer) createRcName() string {
	return deployer.Deployment.AppName + "-" + deployer.Deployment.NewVersion
}

func (deployer *deployer) createReplicationController() error {

	ctrl := new(api.ReplicationController)
	rcName := deployer.createRcName()
	ctrl.Name = rcName

	labels := make(map[string]string)
	labels["name"] = deployer.Deployment.AppName
	labels["version"] = deployer.Deployment.NewVersion

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

	callBack := make(chan string)
	timeout := make(chan string)

	go deployer.watchPods(rcName, deployer.Deployment.NewVersion, callBack)
	go func() {
		time.Sleep(30 * time.Second)
		timeout <- "TIMEOUT"
	}()

	log.Println("Creating Replication Controller")
	_, error := deployer.k8client.ReplicationControllers(api.NamespaceDefault).Create(ctrl)
	if error != nil {
		return error
	}

	select {
	case <- callBack:
	case <- timeout:
		return errors.New("Timeout waiting for pods")
	}

	return nil
}


func (deployer *deployer) watchPods(name, version string, callback chan string) error {
	podSelector := labels.Set{"name": name, "version": deployer.Deployment.NewVersion}.AsSelector()
	watchNew, err := deployer.k8client.Pods(api.NamespaceDefault).Watch(podSelector, fields.Everything(), "0")

	if err != nil {
		return err
	}

	watchChan := watchNew.ResultChan()

	for pod := range watchChan {
		podObj := pod.Object.(*api.Pod)

		if podObj.Status.Phase == "Running" {

			port := podObj.Spec.Containers[0].Ports[0].ContainerPort
			host := podObj.Status.PodIP

			if deployer.Deployment.UseHealthCheck {
				healthy := healthcheck.WaitForPodStarted(host, port, time.Second * 20)
				if !healthy {
					return errors.New("Pod didn't get healthy")
				}
			}

			pods, listErr := deployer.k8client.Pods(api.NamespaceDefault).List(podSelector, fields.Everything())
			if listErr != nil {
				return err
			}

			nrOfPods := deployer.countRunning(pods.Items)
			if nrOfPods == deployer.Deployment.Replicas {
				log.Printf("Found enough running pods (%v), continue to switch versions...\n", nrOfPods)
				watchNew.Stop()
				callback <- "FINISHED"
				break
			} else {
				log.Printf("Waiting for %v more pods...\n", deployer.Deployment.Replicas - nrOfPods)
			}
		}
	}

	return nil

}


func (deployer *deployer) countRunning(pods []api.Pod) int {
	nrOfRunning := 0

	for _,pod := range pods {
		if pod.Status.Phase == "Running" {
			nrOfRunning++
		}
	}

	return nrOfRunning
}

type Backend struct {
	Type string
	BackendId string
	Route string
}

type BackendSettings struct {
	MaxIdleConnsPerHost int
}

type BackendConfig struct {
	Type string
	Settings BackendSettings
}

func (deployer *deployer) prepareNewVulcanBackend() error {
	keyName := fmt.Sprintf("/vulcan/backends/%v/backend", deployer.createRcName())

	backend := BackendConfig {
		Type: "http",
		Settings: BackendSettings{
			MaxIdleConnsPerHost: 128,
		},
	}

	strValue, err := json.Marshal(backend)
	if err != nil {
		return err
	}

	deployer.etcdClient.Set(keyName, string(strValue), 0)

	return nil
}

func (deployer *deployer) switchVulcanBackend() error {

	keyName := fmt.Sprintf("/vulcan/frontends/%v/frontend", deployer.Deployment.VulcanFrontend)

	frontend, err := deployer.etcdClient.Get(keyName, false, false)
	if err != nil {
		return err
	}

	dec := json.NewDecoder(strings.NewReader(frontend.Node.Value))
	var backend Backend
	dec.Decode(&backend)

	backend.BackendId = deployer.createRcName()

	strValue, err := json.Marshal(backend)
	if err != nil {
		return err
	}

	deployer.etcdClient.Set(keyName, string(strValue), 0)

	return nil
}


func (deployer *deployer) cleaupOldDeployments() {
	controllers, err := deployer.findCurrentRc()

	if err != nil {
		log.Println("Did not find a old Replication Controller to remove")
		return
	}

	for _,rc := range controllers {
		deployer.deleteRc(rc)
		deployer.deleteVulcanBackend(rc)
	}

}

func (deployer *deployer) deleteRc(rc api.ReplicationController) {
	log.Printf("Deleting RC %v", rc.Name)

	rc.Spec.Replicas = 0
	deployer.k8client.ReplicationControllers(api.NamespaceDefault).Update(&rc)
	deployer.k8client.ReplicationControllers(api.NamespaceDefault).Delete(rc.Name)
}

func (deployer *deployer) deleteVulcanBackend(rc api.ReplicationController) {
	backendName := fmt.Sprintf("%v-%v", rc.Labels["name"], rc.Labels["version"])
	keyName := fmt.Sprintf("/vulcan/backends/%v", backendName)

	deployer.etcdClient.Delete(keyName, true)
}

func (deployer *deployer)findCurrentRc() ([]api.ReplicationController, error) {
	result := make([]api.ReplicationController, 1, 10)

	rcLabelSelector := labels.Set{"name": deployer.Deployment.AppName}.AsSelector()
	replicationControllers,_ := deployer.k8client.ReplicationControllers(api.NamespaceDefault).List(rcLabelSelector)

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