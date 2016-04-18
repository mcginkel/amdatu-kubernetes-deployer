/*
amdatu-kubernetes-go is a client library for Kubernetes. It uses the Kuberentes REST API internally.

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
package client

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-go/api/unversioned"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-go/api/v1"
	"golang.org/x/net/websocket"
)

type Client struct {
	HttpClient *http.Client
	Url        string
	Username   string
	Password   string
}

type Labels map[string]string

// NewClient creates a new Client. If authentication on the API server is disabled, the username/password arguments can remain empty strings.
func NewClient(url, username, password string) Client {
	httpClient := &http.Client{}

	return Client{HttpClient: httpClient, Url: url, Username: username, Password: password}
}

// NewClientWithHttpClient creates a new Client, based on a pre-configured httpClient.
// This is useful for configuring SSL for example.
// If authentication on the API server is disabled, the username/password arguments can remain empty strings.
func NewClientWithHttpClient(httpClient *http.Client, url, username, password string) Client {
	return Client{HttpClient: httpClient, Url: url, Username: username, Password: password}
}

// ListPods lists pods within a namespace. If no namespace is specified (empty string), it returns all pods in all namespaces.
func (c *Client) ListPods(namespace string) (*v1.PodList, error) {

	result := v1.PodList{}

	var url string

	if namespace == "" {
		url = c.Url + "/api/v1/pods"
	} else {
		url = c.Url + "/api/v1/namespaces/" + namespace + "/pods"
	}

	err := c.get(url, &result)

	if err != nil {
		return &v1.PodList{}, err
	}

	return &result, nil
}

// ListPodsWithLabel lists pods within a namespace, filtered by the pod's labels.
// If no namespace is specified (empty string), it returns all pods in all namespaces, filtered by labels.
func (c *Client) ListPodsWithLabel(namespace string, labels Labels) (*v1.PodList, error) {

	result := v1.PodList{}

	url := c.Url + "/api/v1/namespaces/" + namespace + "/pods"
	url = getUrlWithLabelQueryParam(url, labels)

	err := c.get(url, &result)

	if err != nil {
		return &v1.PodList{}, err
	}

	return &result, nil
}

// WatchPodsWithLabel watches pods, filtered by labels. This starts a new go routine, and results are posted to the returned channel.
// The second return value is a signal channel, which can be used to cancel the watch.
func (c *Client) WatchPodsWithLabel(namespace string, labels Labels) (chan PodEvent, chan string, error) {
	url := c.Url + "/api/v1/namespaces/" + namespace + "/pods"

	podList := v1.PodList{}
	c.get(url, &podList)

	url = getUrlWithLabelQueryParam(url, labels)
	url = url + "&watch=true&resourceVersion=" + podList.ResourceVersion

	url = strings.Replace(url, "http", "ws", 1)

	log.Println(url)

	ws, err := websocket.Dial(url, "", "localhost:8000")

	if err != nil {
		return nil, nil, err
	}

	eventChannel := make(chan PodEvent)
	signalChannel := make(chan string)

	go watch(ws, eventChannel, signalChannel)

	return eventChannel, signalChannel, nil
}

func watch(ws *websocket.Conn, events chan PodEvent, signals chan string) {
	for {

		select {
		case <-signals:
			close(events)
			close(signals)
			ws.Close()
			return

		default:
			var event PodEvent
			err := websocket.JSON.Receive(ws, &event)
			if err != nil {
				signals <- err.Error()
				close(events)
				close(signals)
			}

			events <- event
		}
	}
}

type EventType string

const (
	Added    EventType = "ADDED"
	Modified EventType = "MODIFIED"
	Deleted  EventType = "DELETED"
	Error    EventType = "ERROR"
)

type PodEvent struct {
	Type EventType

	// Object is:
	//  * If Type is Added or Modified: the new state of the object.
	//  * If Type is Deleted: the state of the object immediately before deletion.
	//  * If Type is Error: *api.Status is recommended; other types may make sense
	//    depending on context.
	Object v1.Pod
}

// UpdatePod updates a pod and return the updated version
// Note that this method updates uses a PUT, so the existing object is overwritten.
func (c *Client) UpdatePod(namespace string, pod *v1.Pod) (*v1.Pod, error) {
	result := v1.Pod{}
	url := c.Url + "/api/v1/namespaces/" + namespace + "/pods/" + pod.Name
	err := c.put(url, &pod, &result)

	if err != nil {
		return &v1.Pod{}, err
	}

	return &result, nil
}

// GetPod finds a pod by name within the namespace
// When the pod is not found an error will be returned.
func (c *Client) GetPod(namespace, podName string) (*v1.Pod, error) {
	result := v1.Pod{}
	url := c.Url + "/api/v1/namespaces/" + namespace + "/pods/" + podName

	err := c.get(url, &result)

	if err != nil {
		return &v1.Pod{}, err
	}

	return &result, nil
}

// DeletePod deletes a pod by name within the namespace
func (c *Client) DeletePod(namespace, pod string) error {
	url := c.Url + "/api/v1/namespaces/" + namespace + "/pods/" + pod

	return c.delete(url)
}

// ListReplicationControllers lists replication controllers within a namespace
func (c *Client) ListReplicationControllers(namespace string) (*v1.ReplicationControllerList, error) {

	result := v1.ReplicationControllerList{}
	url := c.Url + "/api/v1/namespaces/" + namespace + "/replicationcontrollers"

	err := c.get(url, &result)

	if err != nil {
		return &v1.ReplicationControllerList{}, err
	}

	return &result, nil
}

// GetReplicationController finds a replication controller by name within a namespace
// When the replication controller is not found an error will be returned.
func (c *Client) GetReplicationController(namespace, replicationController string) (*v1.ReplicationController, error) {

	result := v1.ReplicationController{}
	url := c.Url + "/api/v1/namespaces/" + namespace + "/replicationcontrollers/" + replicationController

	err := c.get(url, &result)

	if err != nil {
		return &v1.ReplicationController{}, err
	}

	return &result, nil
}

// ListReplicationControllersWithLabel finds replication controllers by name within a namespace, filtered by labels.
func (c *Client) ListReplicationControllersWithLabel(namespace string, labels Labels) (*v1.ReplicationControllerList, error) {

	result := v1.ReplicationControllerList{}
	url := c.Url + "/api/v1/namespaces/" + namespace + "/replicationcontrollers"
	url = getUrlWithLabelQueryParam(url, labels)

	err := c.get(url, &result)

	if err != nil {
		return &v1.ReplicationControllerList{}, err
	}

	return &result, nil
}

// CreateReplicationController creates a new replication controller within the namespace.
// Returns the created resource.
func (c *Client) CreateReplicationController(namespace string, rc *v1.ReplicationController) (*v1.ReplicationController, error) {

	result := v1.ReplicationController{}
	url := c.Url + "/api/v1/namespaces/" + namespace + "/replicationcontrollers"
	err := c.post(url, &rc, &result)

	if err != nil {
		return &v1.ReplicationController{}, err
	}

	return &result, nil
}

// UpdateReplicationController updates a replication controller within the namespace.
// Note that this method updates uses a PUT, so the existing object is overwritten.
func (c *Client) UpdateReplicationController(namespace string, rc *v1.ReplicationController) (*v1.ReplicationController, error) {
	result := v1.ReplicationController{}
	url := c.Url + "/api/v1/namespaces/" + namespace + "/replicationcontrollers/" + rc.Name
	err := c.put(url, &rc, &result)

	if err != nil {
		return &v1.ReplicationController{}, err
	}

	return &result, nil
}

// DeleteReplicationController deletes a replication controller by name within a namespace.
func (c *Client) DeleteReplicationController(namespace, replicationController string) error {

	url := c.Url + "/api/v1/namespaces/" + namespace + "/replicationcontrollers/" + replicationController

	return c.delete(url)
}

func (c *Client) CreateNamespace(namespace string) (*v1.Namespace, error) {
	ns := v1.Namespace{
		ObjectMeta: v1.ObjectMeta{Name: namespace},
	}

	result := v1.Namespace{}

	url := c.Url + "/api/v1/namespaces"
	err := c.post(url, &ns, &result)

	if err != nil {
		return &v1.Namespace{}, err
	}

	return &result, err
}

func (c *Client) DeleteNamespace(namespace string) error {

	url := c.Url + "/api/v1/namespaces/" + namespace

	return c.delete(url)
}

func (c *Client) ListNamespaces() (*v1.NamespaceList, error) {
	result := v1.NamespaceList{}
	url := c.Url + "/api/v1/namespaces"

	err := c.get(url, &result)

	if err != nil {
		return &v1.NamespaceList{}, err
	}

	return &result, nil
}

// ListServices lists services within a namespace
func (c *Client) ListServices(namespace string) (*v1.ServiceList, error) {
	result := v1.ServiceList{}
	url := c.Url + "/api/v1/namespaces/" + namespace + "/services"

	err := c.get(url, &result)

	if err != nil {
		return &v1.ServiceList{}, err
	}

	return &result, nil
}

// ListServicesWithLabel lists services within a namespace, filtered by labels.
func (c *Client) ListServicesWithLabel(namespace string, labels Labels) (*v1.ServiceList, error) {
	result := v1.ServiceList{}
	url := c.Url + "/api/v1/namespaces/" + namespace + "/services"
	url = getUrlWithLabelQueryParam(url, labels)

	err := c.get(url, &result)

	if err != nil {
		return &v1.ServiceList{}, err
	}

	return &result, nil
}

// GetService finds a service by name within a namespace.
// When the service is not found an error will be returned.
func (c *Client) GetService(namespace, service string) (*v1.Service, error) {
	result := v1.Service{}
	url := c.Url + "/api/v1/namespaces/" + namespace + "/services/" + service

	err := c.get(url, &result)

	if err != nil {
		return &v1.Service{}, err
	}

	return &result, nil
}

func (c *Client) ListNodes() (*v1.NodeList, error) {
	result := v1.NodeList{}
	url := c.Url + "/api/v1/nodes"
	err := c.get(url, &result)

	if err != nil {
		return &v1.NodeList{}, err
	}

	return &result, nil
}

func (c *Client) GetNode(node string) (*v1.Node, error) {
	result := v1.Node{}
	url := c.Url + "/api/v1/nodes/" + node

	err := c.get(url, &result)

	if err != nil {
		return &v1.Node{}, err
	}

	return &result, nil
}

// UpdateNode updates a node.
// Note that this method updates uses a PUT, so the existing object is overwritten.
func (c *Client) UpdateNode(node *v1.Node) (*v1.Node, error) {

	result := v1.Node{}
	url := c.Url + "/api/v1/nodes/" + node.Name
	err := c.put(url, &node, &result)

	if err != nil {
		return &v1.Node{}, err
	}

	return &result, nil
}

func (c *Client) DeleteNode(node string) error {
	url := c.Url + "/api/v1/nodes/" + node
	return c.delete(url)
}

func getUrlWithLabelQueryParam(url string, labels Labels) string {
	if len(labels) > 0 {
		buffer := bytes.Buffer{}

		first := true

		for k, v := range labels {
			if !first {
				buffer.WriteString(",")
			}

			first = false

			buffer.WriteString(k + "%3D" + v)
		}

		url = url + "?labelSelector=" + buffer.String()
	}

	return url
}

func (c *Client) CreateService(namespace string, service *v1.Service) (*v1.Service, error) {
	result := v1.Service{}

	url := c.Url + "/api/v1/namespaces/" + namespace + "/services"
	err := c.post(url, service, &result)

	if err != nil {
		return &v1.Service{}, err
	}

	return &result, err
}

func (c *Client) DeleteService(namespace, service string) error {
	url := c.Url + "/api/v1/namespaces/" + namespace + "/services/" + service
	return c.delete(url)
}

func (c *Client) Patch(namespace, resourceType, objectName, jsonPatch string) error {
	jsonBytes := []byte(jsonPatch)

	url := c.Url + "/api/v1/namespaces/" + namespace + "/" + resourceType +"/" + objectName
	log.Printf("Requesting patch on %v\n", url)

	req, err := http.NewRequest("PATCH", url, bytes.NewBuffer(jsonBytes))

	if err != nil {
		log.Printf("Error scaling down replication controller: %v\n", err.Error())
	}

	req.Header.Add("Content-Type", "application/merge-patch+json")

	_, err = c.HttpClient.Do(req)
	return err
}



func (c *Client) createRequest(method, url string, body io.Reader) (*http.Request, error) {
	request, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	request.Header.Add("Authorization", createBasicAuthHeader(c.Username, c.Password))

	return request, nil
}

func (c *Client) get(url string, result interface{}) error {
	log.Printf("Requesting GET %v\n", url)

	req, err := c.createRequest("GET", url, nil)
	if err != nil {
		return err
	}

	resp, err := c.HttpClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 300 {
		printResponse(resp)

		return errors.New("Http request return status code: " + resp.Status)
	}

	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&result)

	if err != nil {
		return err
	}
	return nil
}

func (c *Client) delete(url string) error {

	log.Printf("Requesting DELETE %v\n", url)

	req, err := c.createRequest("DELETE", url, nil)
	if err != nil {
		return err
	}

	resp, err := c.HttpClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 300 {
		printResponse(resp)

		return errors.New("Http request return status code: " + resp.Status)
	}

	return nil
}

func (c *Client) post(url string, body interface{}, result interface{}) error {

	log.Printf("Requesting POST %v\n", url)

	b, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := c.createRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := c.HttpClient.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)

	if resp.StatusCode >= 300 {

		status := new(unversioned.Status)
		err = decoder.Decode(&status)
		if err != nil {
			return errors.New("Http request error: " + resp.Status)
		} else {
			return errors.New("Http request error, code: " + resp.Status + ", message: " + status.Message)
		}
	}

	err = decoder.Decode(&result)

	if err != nil {
		printResponse(resp)

		return err
	}
	return nil
}

func (c *Client) put(url string, body interface{}, result interface{}) error {

	log.Printf("Requesting PUT %v\n", url)

	b, err := json.Marshal(body)
	if err != nil {
		return err
	}

	req, err := c.createRequest("PUT", url, bytes.NewReader(b))
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/json")

	resp, err := c.HttpClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode >= 300 {
		printResponse(resp)

		return errors.New("Http request return status code: " + resp.Status)
	}

	defer resp.Body.Close()
	decoder := json.NewDecoder(resp.Body)
	err = decoder.Decode(&result)

	if err != nil {
		printResponse(resp)

		return err
	}
	return nil
}

func printResponse(resp *http.Response) {
	contents, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Printf("%s", err)
		os.Exit(1)
	}
	log.Printf("%s\n", string(contents))
}

func createBasicAuthHeader(username, password string) string {
	basicAuth := base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
	return "Bearer " + basicAuth
}
