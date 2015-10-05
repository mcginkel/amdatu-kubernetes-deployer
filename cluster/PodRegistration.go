package cluster

import (
	"com.amdatu.rti.deployment/Godeps/_workspace/src/k8s.io/kubernetes/pkg/api"
	"com.amdatu.rti.deployment/Godeps/_workspace/src/k8s.io/kubernetes/pkg/client/unversioned"
	"com.amdatu.rti.deployment/Godeps/_workspace/src/k8s.io/kubernetes/pkg/fields"
	"com.amdatu.rti.deployment/Godeps/_workspace/src/k8s.io/kubernetes/pkg/labels"
	"com.amdatu.rti.deployment/proxies"
	"fmt"
)

func StartWatching(k8client *unversioned.Client, proxyConfigurator *proxies.ProxyConfigurator) {
	errorChannel := make(chan string)

	go watch(k8client, proxyConfigurator, errorChannel)
	go reconnector(k8client, proxyConfigurator, errorChannel)
}

func watch(K8client *unversioned.Client, ProxyConfigurator *proxies.ProxyConfigurator, errorChannel chan string) {
	podList, err := K8client.Pods("").List(labels.Everything(), fields.Everything())
	resourceVersion := podList.ResourceVersion


	fmt.Printf("Resource version for Pod watch: %v\n", resourceVersion)
	w, err := K8client.Pods("").Watch(labels.Everything(), fields.Everything(), resourceVersion)

	if err != nil {
		fmt.Printf("Error creating watch on pods: %v", err)
		return
	}

	channel := w.ResultChan()

	fmt.Println("Watching for pods...")
	for pod := range channel {
		podObj := pod.Object.(*api.Pod)

		if pod.Type == "ERROR" {
			errorChannel <- "Disconnect"
			fmt.Println("Error received from Kubernetes")
			return
		}

		backendName := fmt.Sprintf("%v-%v", podObj.Namespace, podObj.Labels["name"])

		if pod.Type == "MODIFIED" && podObj.Status.Phase == "Running" {

			fmt.Printf("Received MODIFIED event for running pod %v\n", podObj.Name)
			ProxyConfigurator.AddBackendServer(backendName, podObj.Status.PodIP, podObj.Spec.Containers[0].Ports[0].ContainerPort)

		} else if pod.Type == "DELETED" {

			ProxyConfigurator.DeleteBackendServer(backendName, podObj.Status.PodIP)

		} else {
			fmt.Printf("Received event of type: %v\n", pod.Type)
		}
	}

	fmt.Println("Channel shut down")
	errorChannel <- "Disconnect"
}

func reconnector(k8client *unversioned.Client, proxyConfigurator *proxies.ProxyConfigurator, errorChannel chan string) {
	for _ = range errorChannel {
		fmt.Println("Reconnecting watch")
		go watch(k8client, proxyConfigurator, errorChannel)
	}

}
