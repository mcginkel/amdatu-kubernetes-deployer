package rolling

import (
	"com.amdatu.rti.deployment/Godeps/_workspace/src/k8s.io/kubernetes/pkg/api"
	"com.amdatu.rti.deployment/Godeps/_workspace/src/k8s.io/kubernetes/pkg/fields"
	"com.amdatu.rti.deployment/Godeps/_workspace/src/k8s.io/kubernetes/pkg/labels"
	"com.amdatu.rti.deployment/cluster"
	"errors"
	"time"
)

type rollingdeploy struct {
	deployer *cluster.Deployer
}

func NewRollingDeployer(deployer *cluster.Deployer) *rollingdeploy {
	return &rollingdeploy{deployer}
}

func (rollingdeploy *rollingdeploy) Deploy() error {

	rollingdeploy.deployer.Logger.Println("Starting rolling deployment")

	newRc, err := rollingdeploy.deployer.CreateReplicationController()
	if err != nil {
		return err
	}

	currentRc, err := rollingdeploy.findCurrentRc()
	if err != nil {
		rollingdeploy.deployer.Logger.Println("No active Replication Controller found, nothing else to do.")
		return nil
	}

	rollingdeploy.deployer.Logger.Printf("Active RC %v with %v replicas. Starting scale down...\n", currentRc.Name, currentRc.Status.Replicas)

	newRcLabelSelector := labels.Set{"name": newRc.Labels["name"], "version": rollingdeploy.deployer.Deployment.NewVersion}.AsSelector()

	podList, err := rollingdeploy.deployer.K8client.Pods(rollingdeploy.deployer.Deployment.Namespace).List(newRcLabelSelector, fields.Everything())

	if err != nil {
		rollingdeploy.deployer.Logger.Println(err)
		return err
	}

	watchNew, err := rollingdeploy.deployer.K8client.Pods(rollingdeploy.deployer.Deployment.Namespace).Watch(newRcLabelSelector, fields.Everything(), podList.ResourceVersion)
	if err != nil {
		return err
	}

	newWatchChannel := watchNew.ResultChan()

	for pod := range newWatchChannel {
		podObj := pod.Object.(*api.Pod)
		if podObj.Status.Phase == "Running" {
			if err := rollingdeploy.deployer.CheckPodHealth(podObj); err != nil {
				return err
			}

			rollingdeploy.deployer.Logger.Printf("Found new running pod %v, continue down scaling\n", podObj.Name)

			if currentRc.Spec.Replicas > 0 {
				currentRc.Spec.Replicas -= 1
				rc, err := rollingdeploy.deployer.K8client.ReplicationControllers(rollingdeploy.deployer.Deployment.Namespace).Update(&currentRc)
				if err != nil {
					rollingdeploy.deployer.Logger.Printf("Error updating existing controller: %v", err)
				}

				currentRc = *rc
			} else {
				rollingdeploy.deployer.K8client.ReplicationControllers(rollingdeploy.deployer.Deployment.Namespace).Delete(currentRc.Name)
				rollingdeploy.deployer.Logger.Printf("Deleted %v", currentRc.Name)
				break
			}

			pods, listErr := rollingdeploy.deployer.K8client.Pods(rollingdeploy.deployer.Deployment.Namespace).List(newRcLabelSelector, fields.Everything())
			if listErr != nil {
				watchNew.Stop()
				return err
			}

			if rollingdeploy.deployer.CountRunningPods(pods.Items) == newRc.Spec.Replicas {
				rollingdeploy.deployer.Logger.Println("Found enough running pods, deleting old cluster")
				currentRc.Spec.Replicas = 0
				rc, err := rollingdeploy.deployer.K8client.ReplicationControllers(rollingdeploy.deployer.Deployment.Namespace).Update(&currentRc)
				if err != nil {
					rollingdeploy.deployer.Logger.Printf("Error updating existing controller: %v", err)
				}

				currentRc = *rc
				time.Sleep(20 * time.Second)

				rollingdeploy.deployer.K8client.ReplicationControllers(rollingdeploy.deployer.Deployment.Namespace).Delete(currentRc.Name)
				rollingdeploy.deployer.Logger.Printf("Deleted %v", currentRc.Name)
				break
			}
		}
	}

	watchNew.Stop()
	return nil
}

func (rollingdeploy *rollingdeploy) findCurrentRc() (api.ReplicationController, error) {

	currentRCs, err := rollingdeploy.deployer.FindCurrentRc()
	if err != nil {
		return api.ReplicationController{}, err
	}

	for _, rc := range currentRCs {

		if rc.Status.Replicas > 0 {
			return rc, nil
		}

	}

	return api.ReplicationController{}, errors.New("No active Replica Controller found")
}
