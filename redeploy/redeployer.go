package redeploy
import (
	"com.amdatu.rti.deployment/cluster"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/fields"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"time"
)
/**
	Deployer to re-deploy an existing version.
	Essentially re-deploying only means restarting the currently running pods. All other configuration, like the load balancer and service, should already be available.

	1) In a rolling fashion, kill all existing pods one at a time.
	2) Kubernetes will take care of rescheduling the replicas
 */

type redeployer struct {
	deployer *cluster.Deployer
}

func NewRedeployer(deployer *cluster.Deployer) *redeployer{
	return &redeployer{deployer}
}

func (redeployer *redeployer) Deploy() error {
	redeployer.deployer.Logger.Println("Redeploying")

	pods, error := redeployer.deployer.FindCurrentPods(true)

	if error != nil {
		redeployer.deployer.Logger.Println(error)
		return error
	}

	podNames := make(map[string]*api.Pod, len(pods))
	for _, p := range pods {
		podNames[p.Name] = &p
	}


	for _,pod := range pods {

		callBack := make(chan bool)
		go redeployer.waitForNewPod(callBack, podNames)

		redeployer.deployer.Logger.Printf("Deleting pod %v\n", pod.ObjectMeta.Name)
		redeployer.deployer.DeletePod(pod)

		<-callBack

		close(callBack)
	}

	return nil
}

func (redeployer *redeployer) waitForNewPod(callback chan bool, existingPods map[string]*api.Pod) {
	podSelector := labels.Set{"name": redeployer.deployer.CreateRcName(), "version": redeployer.deployer.Deployment.NewVersion}.AsSelector()
	podList, err := redeployer.deployer.K8client.Pods(redeployer.deployer.Deployment.Namespace).List(podSelector, fields.Everything())

	if err != nil {
		redeployer.deployer.Logger.Println(err)
		return
	}

	watchNew, err := redeployer.deployer.K8client.Pods(redeployer.deployer.Deployment.Namespace).Watch(podSelector, fields.Everything(), podList.ResourceVersion)

	if err != nil {
		redeployer.deployer.Logger.Println(err)
		callback <- false
		return
	}

	watchChan := watchNew.ResultChan()

	timeout := make(chan string)
	go func() {
		time.Sleep(time.Minute * 5)
		timeout <- "TIMEOUT"
		close(timeout)
		watchNew.Stop()

		callback <- false
	}()

	for evnt := range watchChan {
		podObj := evnt.Object.(*api.Pod)

		if evnt.Type == "MODIFIED" && existingPods[podObj.Name] == nil && podObj.Status.PodIP != "" {
			redeployer.deployer.CheckPodHealth(podObj)
			redeployer.deployer.Logger.Println("Found new pod")
			callback <- true
			watchNew.Stop()
			break;
		}
	}
}
