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
	go watch(k8client, proxyConfigurator)
}

func watch(K8client *unversioned.Client, ProxyConfigurator *proxies.ProxyConfigurator) {
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

		backendName := fmt.Sprintf("%v-%v", podObj.Namespace, podObj.Labels["name"])

		if pod.Type == "MODIFIED" && podObj.Status.Phase == "Running" {

			ProxyConfigurator.AddBackendServer(backendName, podObj.Status.PodIP, podObj.Spec.Containers[0].Ports[0].ContainerPort)

		} else if pod.Type == "DELETED" {

			ProxyConfigurator.DeleteBackendServer(backendName, podObj.Status.PodIP)

		}
	}
}