package rolling
import (
	"com.amdatu.rti.deployment/cluster"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/fields"
	"errors"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
)


type rollingdeploy struct {
	deployer *cluster.Deployer
}

func NewRollingDeployer(deployer *cluster.Deployer) *rollingdeploy{
	return &rollingdeploy{deployer}
}

func (rollingdeploy *rollingdeploy) Deploy() error {

	rollingdeploy.deployer.Logger.Println("Starting rolling deployment")

	newRc, err := rollingdeploy.deployer.CreateReplicationController()
	if err != nil{
		return err
	}

	currentRc, err := rollingdeploy.findCurrentRc()
	if err != nil {
		return err
	}

	rollingdeploy.deployer.Logger.Printf("Active RC %v with %v replicas. Starting scale down...\n", currentRc.Name, currentRc.Status.Replicas)
	rcLabelSelector := labels.Set{"name": currentRc.Name}.AsSelector()
	watchCurrent, err := rollingdeploy.deployer.K8client.ReplicationControllers(api.NamespaceDefault).Watch(rcLabelSelector, fields.Everything(), "0")

	if(err != nil) {
		return err
	}

	watchChannel := watchCurrent.ResultChan()

	ci := make(chan string)
	closeChannel := make(chan bool)
	
	go  rollingdeploy.watchNewPods(newRc, ci, closeChannel)

	for r := range watchChannel{
		wRC := r.Object.(*api.ReplicationController)

		rollingdeploy.deployer.Logger.Printf("Old Replication Controller %v has %v replicas\n", wRC.Name, wRC.Status.Replicas)

		if wRC.Spec.Replicas == 0 {
			watchCurrent.Stop()
			rollingdeploy.deployer.Logger.Println("Downscaling completed")
			rollingdeploy.deployer.CleaupOldDeployments()
			closeChannel <- true

			return nil
		} else {
			wRC.Spec.Replicas -= 1
			rollingdeploy.deployer.Logger.Printf("Scaling down to %v replicas\n", wRC.Spec.Replicas)
			rollingdeploy.deployer.K8client.ReplicationControllers(api.NamespaceDefault).Update(wRC)
			rollingdeploy.deployer.Logger.Println("Waiting for new RC to spin up pods")

			result := <- ci
			if result == "ERROR" {
				closeChannel <- true
				return errors.New("Error spinning up new pod")
			}
		}


	}

	return nil
}

func (rollingdeploy *rollingdeploy) watchNewPods(newRc *api.ReplicationController, ci chan string, closeChannel chan bool) {
	newRcLabelSelector := labels.Set{"name": newRc.Labels["name"], "version": rollingdeploy.deployer.Deployment.NewVersion}.AsSelector()
	watchNew, err := rollingdeploy.deployer.K8client.Pods(api.NamespaceDefault).Watch(newRcLabelSelector, fields.Everything(), "0")
	if(err != nil) {
		rollingdeploy.deployer.Logger.Println(err)
		return
	}

	newWatchChannel := watchNew.ResultChan()

	for pod := range newWatchChannel {
		podObj := pod.Object.(*api.Pod)
		if podObj.Status.Phase == "Running" {
			if err := rollingdeploy.deployer.CheckPodHealth(podObj); err != nil {
				ci <- "ERROR"
			}


			rollingdeploy.deployer.Logger.Println("Found new running pod, continue down scaling")
			ci <- podObj.Name

			pods, listErr := rollingdeploy.deployer.K8client.Pods(api.NamespaceDefault).List(newRcLabelSelector, fields.Everything())
			if listErr != nil {
				return err
			}

			if rollingdeploy.deployer.CountRunningPods(pods.Items) == newRc.Spec.Replicas {
				return
			}
		}
	}
}

func (rollingdeploy *rollingdeploy)findCurrentRc() (api.ReplicationController, error) {

	currentRCs, err := rollingdeploy.deployer.FindCurrentRc()
	if err != nil {
		return api.ReplicationController{}, err
	}

	for _,rc := range currentRCs {

		if(rc.Status.Replicas > 0) {
			return rc,nil
		}

	}

	return api.ReplicationController{}, errors.New("No active Replica Controller found")
}