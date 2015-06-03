package bluegreen

/**
1) Create new RC, with a label name that includes the version.
2) Watch Kubernetes for Pods to become RUNNING
3) Switch backend in Vulcan
4) Remove old backend from Vulcan
5) Remove old RC from Kubernetes
 */

import (
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"encoding/json"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/fields"
	"time"
	"fmt"
	"strings"
	"errors"
	"com.amdatu.rti.deployment/cluster"
)

type bluegreen struct {
	deployer *cluster.Deployer
}

func NewBlueGreen(deployer *cluster.Deployer) *bluegreen{
	return &bluegreen{deployer}
}

func (bluegreen *bluegreen) Deploy() error {

	bluegreen.deployer.Logger.Println("Starting blue-green deployment")

	bluegreen.deployer.Logger.Println("Prepare vulcan backend....")

	if err := bluegreen.prepareNewVulcanBackend(); err != nil {
		return err
	}

	if err := bluegreen.createReplicationController(); err != nil {
		bluegreen.deployer.Logger.Printf("%", err)
		return err
	}

	bluegreen.deployer.Logger.Println("Switch vulcan backends....")
	if err := bluegreen.switchVulcanBackend(); err != nil {
		return err
	}

	bluegreen.deployer.Logger.Println("Cleaning up old deployments....")
	bluegreen.deployer.CleaupOldDeployments()

	return nil
}

func (bluegreen *bluegreen) createReplicationController() error {
	bluegreen.deployer.CreateReplicationController()

	callBack := make(chan string)
	timeout := make(chan string)

	go bluegreen.watchPods(bluegreen.deployer.CreateRcName(), bluegreen.deployer.Deployment.NewVersion, callBack)
	go func() {
		time.Sleep(10 * time.Minute)
		timeout <- "TIMEOUT"
	}()

	//Wait for either the pods to report healthy, or the timeout to happen
	select {
	case <- callBack:
	case <- timeout:
		return errors.New("Timeout waiting for pods")
	}

	return nil
}


func (bluegreen *bluegreen) watchPods(name, version string, callback chan string) error {
	podSelector := labels.Set{"name": name, "version": bluegreen.deployer.Deployment.NewVersion}.AsSelector()
	watchNew, err := bluegreen.deployer.K8client.Pods(api.NamespaceDefault).Watch(podSelector, fields.Everything(), "0")

	if err != nil {
		return err
	}

	watchChan := watchNew.ResultChan()

	for pod := range watchChan {
		podObj := pod.Object.(*api.Pod)

		if podObj.Status.Phase == "Running" {

			bluegreen.deployer.CheckPodHealth(podObj)

			pods, listErr := bluegreen.deployer.K8client.Pods(api.NamespaceDefault).List(podSelector, fields.Everything())
			if listErr != nil {
				return err
			}

			nrOfPods := bluegreen.deployer.CountRunningPods(pods.Items)
			if nrOfPods == bluegreen.deployer.Deployment.Replicas {
				bluegreen.deployer.Logger.Printf("Found enough running pods (%v), continue to switch versions...\n", nrOfPods)
				watchNew.Stop()
				callback <- "FINISHED"
				return nil
			} else {
				bluegreen.deployer.Logger.Printf("Waiting for %v more pods...\n", bluegreen.deployer.Deployment.Replicas - nrOfPods)
			}
		}
	}

	return nil
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

func (bluegreen *bluegreen) prepareNewVulcanBackend() error {
	keyName := fmt.Sprintf("/vulcan/backends/%v/backend", bluegreen.deployer.CreateRcName())

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

	bluegreen.deployer.EtcdClient.Set(keyName, string(strValue), 0)

	return nil
}

func (bluegreen *bluegreen) switchVulcanBackend() error {

	keyName := fmt.Sprintf("/vulcan/frontends/%v/frontend", bluegreen.deployer.Deployment.VulcanFrontend)

	bluegreen.deployer.Logger.Printf("Switching backend for frontend config %v, using etcd at %v", keyName, bluegreen.deployer.EtcdUrl)

	frontend, err := bluegreen.deployer.EtcdClient.Get(keyName, false, false)
	if err != nil {
		return err
	}

	dec := json.NewDecoder(strings.NewReader(frontend.Node.Value))
	var backend Backend
	dec.Decode(&backend)

	backend.BackendId = bluegreen.deployer.CreateRcName()

	strValue, err := json.Marshal(backend)
	if err != nil {
		return err
	}

	bluegreen.deployer.EtcdClient.Set(keyName, string(strValue), 0)

	return nil
}



