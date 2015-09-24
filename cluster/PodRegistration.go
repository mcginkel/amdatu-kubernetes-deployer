package cluster
import (
	"k8s.io/kubernetes/pkg/client/unversioned"
	"com.amdatu.rti.deployment/proxies"
	"k8s.io/kubernetes/pkg/labels"
	"fmt"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/fields"
)

func StartWatching(k8client *unversioned.Client, proxyConfigurator *proxies.ProxyConfigurator) {
	errorChannel := make(chan string)

	go watch(k8client, proxyConfigurator, errorChannel)
	go reconnector(k8client, proxyConfigurator, errorChannel)
}

func watch(K8client *unversioned.Client, ProxyConfigurator *proxies.ProxyConfigurator, errorChannel chan string) {
	podList, err := K8client.Pods("").List(labels.Everything(), fields.Everything())
	resouceVersion := podList.ResourceVersion

	w, err := K8client.Pods("").Watch(labels.Everything(), fields.Everything(), resouceVersion)

	if err != nil {
		fmt.Printf("Error creating watch on pods: %v", err)
		return
	}

	channel := w.ResultChan()

	for pod := range channel {
		podObj := pod.Object.(*api.Pod)

		if pod.Type == "ERROR" {
			errorChannel <- "Disconnect"

			return
		}

		backendName := fmt.Sprintf("%v-%v", podObj.Namespace, podObj.Labels["name"])

		if pod.Type == "MODIFIED" && podObj.Status.Phase == "Running" {

			if ProxyConfigurator.FrontendExistsForDeployment(backendName) {
				ProxyConfigurator.AddBackendServer(backendName, podObj.Status.PodIP, podObj.Spec.Containers[0].Ports[0].ContainerPort)
			}

		} else if pod.Type == "DELETED" {

			ProxyConfigurator.DeleteBackendServer(backendName, podObj.Status.PodIP)

		}
	}
}

func reconnector(k8client *unversioned.Client, proxyConfigurator *proxies.ProxyConfigurator, errorChannel chan string) {
	for _ = range errorChannel {
		fmt.Println("Reconnecting watch")
		go watch(k8client, proxyConfigurator, errorChannel)
	}

}