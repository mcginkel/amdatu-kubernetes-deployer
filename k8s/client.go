/*
Copyright (c) 2016 The Amdatu Foundation

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package k8s

import (
	"log"
	"time"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/tools/clientcmd"
)

type K8sConfig struct {
	ApiServerUrl string
}

type K8sClient struct {
	client *kubernetes.Clientset
}

func New(k8sConfig K8sConfig) (*K8sClient, error) {

	// create config
	config, err := clientcmd.BuildConfigFromFlags(k8sConfig.ApiServerUrl, "")
	if err != nil {
		log.Fatalf("Error creating k8s config: %v", err.Error())
		return nil, err
	}
	// create client
	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Error creating k8s client: %v", err.Error())
		return nil, err
	}

	k8sClient := K8sClient{
		client: client,
	}

	return &k8sClient, nil
}

func (k8s *K8sClient) ListReplicationControllers(namespace string) (*v1.ReplicationControllerList, error) {
	return k8s.ListReplicationControllersWithSelector(namespace, make(map[string]string))
}

func (k8s *K8sClient) ListReplicationControllersWithSelector(namespace string, selector map[string]string) (*v1.ReplicationControllerList, error) {
	return k8s.client.
		ReplicationControllers(namespace).
		List(meta.ListOptions{
			LabelSelector: labels.SelectorFromSet(selector).String(),
		})
}

func (k8s *K8sClient) DeleteReplicationController(namespace, name string) error {
	return k8s.client.
		ReplicationControllers(namespace).
		Delete(name, &meta.DeleteOptions{})
}

func (k8s *K8sClient) CreateReplicationController(namespace string, rc *v1.ReplicationController) (*v1.ReplicationController, error) {
	return k8s.client.ReplicationControllers(namespace).Create(rc)
}

func (k8s *K8sClient) GetReplicationController(namespace, name string) (*v1.ReplicationController, error) {
	return k8s.client.ReplicationControllers(namespace).Get(name, meta.GetOptions{})
}

func (k8s *K8sClient) UpdateReplicationController(namespace string, rc *v1.ReplicationController) (*v1.ReplicationController, error) {
	return k8s.client.ReplicationControllers(namespace).Update(rc)
}

func (k8s *K8sClient) ListPods(namespace string) (*v1.PodList, error) {
	return k8s.ListPodsWithSelector(namespace, nil)
}

func (k8s *K8sClient) ListPodsWithSelector(namespace string, selector map[string]string) (*v1.PodList, error) {
	return k8s.client.
		Pods(namespace).
		List(meta.ListOptions{
			LabelSelector: labels.SelectorFromSet(selector).String(),
		})
}

func (k8s *K8sClient) DeletePod(namespace, name string) error {
	return k8s.client.
		Pods(namespace).
		Delete(name, &meta.DeleteOptions{})
}

func (k8s *K8sClient) ListServices(namespace string) (*v1.ServiceList, error) {
	return k8s.ListServicesWithSelector(namespace, nil)
}

func (k8s *K8sClient) ListServicesWithSelector(namespace string, selector map[string]string) (*v1.ServiceList, error) {
	return k8s.client.
		Services(namespace).
		List(meta.ListOptions{
			LabelSelector: labels.SelectorFromSet(selector).String(),
		})
}

func (k8s *K8sClient) DeleteService(namespace, name string) error {
	return k8s.client.
		Services(namespace).
		Delete(name, &meta.DeleteOptions{})
}

func (k8s *K8sClient) CreateService(namespace string, svc *v1.Service) (*v1.Service, error) {
	return k8s.client.Services(namespace).Create(svc)
}

func (k8s *K8sClient) GetService(namespace, name string) (*v1.Service, error) {
	return k8s.client.Services(namespace).Get(name, meta.GetOptions{})
}

func (k8s *K8sClient) UpdateService(namespace string, svc *v1.Service) (*v1.Service, error) {
	return k8s.client.Services(namespace).Update(svc)
}

func (k8s *K8sClient) ListNamespaces() (*v1.NamespaceList, error) {
	return k8s.client.Namespaces().List(meta.ListOptions{})
}

func (k8s *K8sClient) DeleteNamespace(name string) error {
	falseVar := false
	return k8s.client.Namespaces().Delete(name, &meta.DeleteOptions{OrphanDependents: &falseVar})
}

func (k8s *K8sClient) CreateNamespace(ns *v1.Namespace) (*v1.Namespace, error) {
	return k8s.client.Namespaces().Create(ns)
}

func (k8s *K8sClient) GetNamespace(name string) (*v1.Namespace, error) {
	return k8s.client.Namespaces().Get(name, meta.GetOptions{})
}

func (k8s *K8sClient) ShutdownReplicationController(rc *v1.ReplicationController, logger logger.Logger) error {
	logger.Printf("Scaling down replication controller: %v\n", rc.Name)

	replicas := int32(0)
	rc.Spec.Replicas = &replicas
	_, err := k8s.client.ReplicationControllers(rc.Namespace).Update(rc)
	if err != nil {
		logger.Printf("Error scaling down replication controller: %v\n", err.Error())
	}

	successChan := make(chan bool)

	go k8s.waitForScaleDown(rc, successChan)

	select {
	case <-successChan:
		logger.Println("Scaledown successful")
	case <-time.After(time.Second * 90):
		logger.Println("Scaledown failed")
		successChan <- false
	}

	return k8s.client.ReplicationControllers(rc.Namespace).Delete(rc.Name, &meta.DeleteOptions{})
}

func (k8s *K8sClient) waitForScaleDown(rc *v1.ReplicationController, successChan chan bool) {
	for {
		select {
		case <-successChan:
			return
		default:

			selector := map[string]string{"app": rc.Labels["app"], "version": rc.Labels["version"]}
			pods, _ := k8s.client.
				Pods(rc.Namespace).
				List(meta.ListOptions{
					LabelSelector: labels.SelectorFromSet(selector).String(),
				})

			if CountRunningPods(pods.Items) > 0 {
				time.Sleep(1 * time.Second)
			} else {
				successChan <- true
				return
			}
		}
	}
}

func CountRunningPods(pods []v1.Pod) int {
	nrOfRunning := 0

	for _, pod := range pods {
		if pod.Status.Phase == "Running" {
			nrOfRunning++
		}
	}

	return nrOfRunning
}
