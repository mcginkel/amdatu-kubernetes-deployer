package bluegreen

/**
1) Create new RC, with a label name that includes the version.
2) Watch Kubernetes for Pods to become RUNNING
3) Switch backend in proxy
4) Remove old backend from proxy
5) Remove old RC from Kubernetes
*/

import (
	"com.amdatu.rti.deployment/Godeps/_workspace/src/k8s.io/kubernetes/pkg/api"
	"com.amdatu.rti.deployment/Godeps/_workspace/src/k8s.io/kubernetes/pkg/fields"
	"com.amdatu.rti.deployment/Godeps/_workspace/src/k8s.io/kubernetes/pkg/labels"
	"com.amdatu.rti.deployment/cluster"
	"com.amdatu.rti.deployment/proxies"
	"errors"
	"log"
	"time"
)

type bluegreen struct {
	deployer *cluster.Deployer
}

func NewBlueGreen(deployer *cluster.Deployer) *bluegreen {
	return &bluegreen{deployer}
}

func (bluegreen *bluegreen) Deploy() error {

	bluegreen.deployer.Logger.Println("Starting blue-green deployment")

	backendId := bluegreen.deployer.Deployment.Namespace + "-" + bluegreen.deployer.CreateRcName()
	bluegreen.deployer.Logger.Printf("Prepare proxy backend %v....\n", backendId)
	if bluegreen.deployer.Deployment.Frontend != "" {
		frontend := proxies.Frontend{
			Type:      "http",
			Hostname:  bluegreen.deployer.Deployment.Frontend,
			BackendId: backendId,
		}

		if _, err := bluegreen.deployer.ProxyConfigurator.CreateFrontEnd(&frontend); err != nil {
			return err
		}
	} else {
		bluegreen.deployer.Logger.Println("No frontend configured in deployment, skipping creation")
	}

	service, err := bluegreen.deployer.CreateService()
	if err != nil {
		log.Println(err)
	}


	if err := bluegreen.createReplicationController(); err != nil {
		bluegreen.deployer.Logger.Println(err)
		return err
	}


	for _,port := range service.Spec.Ports {
		bluegreen.deployer.Logger.Println("Adding backend", port)
		bluegreen.deployer.ProxyConfigurator.AddBackendServer(backendId, service.Spec.ClusterIP, port.Port)
	}

	bluegreen.deployer.Logger.Println("Sleeping for 20 seconds for proxy to reload...")
	time.Sleep(time.Second * 20)

	bluegreen.deployer.Logger.Println("Switch proxy backends....")

	if err := bluegreen.deployer.ProxyConfigurator.SwitchBackend(bluegreen.deployer.Deployment.Frontend, backendId); err != nil {
		bluegreen.deployer.Logger.Printf("%", err)
		return err
	}

	bluegreen.deployer.Logger.Println("Cleaning up old deployments....")
	bluegreen.deployer.CleaupOldDeployments()

	return nil
}

func (bluegreen *bluegreen) createReplicationController() error {

	bluegreen.deployer.CreateReplicationController()

	if bluegreen.deployer.Deployment.Replicas == 0 {
		return nil
	}

	callBack := make(chan string)
	timeout := make(chan string)

	go bluegreen.watchPods(bluegreen.deployer.CreateRcName(), bluegreen.deployer.Deployment.NewVersion, callBack)
	go func() {
		time.Sleep(10 * time.Minute)
		timeout <- "TIMEOUT"
	}()

	//Wait for either the pods to report healthy, or the timeout to happen
	select {
	case msg := <-callBack:
		if msg == "ERROR" {
			return errors.New("Did not find enough running pods")
		}
	case <-timeout:
		return errors.New("Timeout waiting for pods")
	}

	return nil
}

func (bluegreen *bluegreen) watchPods(name, version string, callback chan string) error {
	podSelector := labels.Set{"name": name, "version": bluegreen.deployer.Deployment.NewVersion}.AsSelector()

	podList, err := bluegreen.deployer.K8client.Pods(bluegreen.deployer.Deployment.Namespace).List(podSelector, fields.Everything())

	if err != nil {
		bluegreen.deployer.Logger.Println(err)
		callback <- "ERROR"
		return err
	}

	watchNew, err := bluegreen.deployer.K8client.Pods(bluegreen.deployer.Deployment.Namespace).Watch(podSelector, fields.Everything(), podList.ResourceVersion)

	if err != nil {
		bluegreen.deployer.Logger.Println(err)
		callback <- "ERROR"
		return err
	}

	watchChan := watchNew.ResultChan()
	bluegreen.deployer.Logger.Println("Waiting for pods to spin up...")

	for pod := range watchChan {
		podObj := pod.Object.(*api.Pod)

		if podObj.Status.Phase == "Running" {

			if err := bluegreen.deployer.CheckPodHealth(podObj); err != nil {
				watchNew.Stop()
				callback <- "ERROR"

				return err
			}

			pods, listErr := bluegreen.deployer.K8client.Pods(bluegreen.deployer.Deployment.Namespace).List(podSelector, fields.Everything())
			if listErr != nil {
				bluegreen.deployer.Logger.Println(listErr)
				watchNew.Stop()
				callback <- "ERROR"

				return err
			}

			nrOfPods := bluegreen.deployer.CountRunningPods(pods.Items)
			if nrOfPods == bluegreen.deployer.Deployment.Replicas {
				bluegreen.deployer.Logger.Printf("Found enough running pods (%v), continue to switch versions...\n", nrOfPods)
				watchNew.Stop()
				callback <- "FINISHED"
				return nil
			} else {
				bluegreen.deployer.Logger.Printf("Waiting for %v more pods...\n", bluegreen.deployer.Deployment.Replicas-nrOfPods)
			}
		}
	}

	return nil
}
