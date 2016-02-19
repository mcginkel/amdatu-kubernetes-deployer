package client

import (
	"bytes"
	"com.cloudrti/kubernetesclient/api/v1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/net/websocket"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
)

type Client struct {
	HttpClient *http.Client
	Url        string
	Username   string
	Password   string
}

type Labels map[string]string

func NewClient(url, username, password string) Client {
	httpClient := &http.Client{}

	return Client{HttpClient: httpClient, Url: url, Username: username, Password: password}
}

func NewClientWithHttpClient(httpClient *http.Client, url, username, password string) Client {
	return Client{HttpClient: httpClient, Url: url, Username: username, Password: password}
}

func (c *Client) ListPods(namespace string) (*v1.PodList, error) {

	result := v1.PodList{}

	url := c.Url + "/api/v1/namespaces/" + namespace + "/pods"
	err := c.get(url, &result)

	if err != nil {
		return &v1.PodList{}, err
	}

	return &result, nil
}

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

func (c *Client) DeletePod(namespace, pod string) error {
	url := c.Url + "/api/v1/namespaces/" + namespace + "/pods/" + pod

	return c.delete(url)
}

func (c *Client) ListReplicationControllers(namespace string) (*v1.ReplicationControllerList, error) {

	result := v1.ReplicationControllerList{}
	url := c.Url + "/api/v1/namespaces/" + namespace + "/replicationcontrollers"

	err := c.get(url, &result)

	if err != nil {
		return &v1.ReplicationControllerList{}, err
	}

	return &result, nil
}

func (c *Client) GetReplicationController(namespace, replicationController string) (*v1.ReplicationController, error) {

	result := v1.ReplicationController{}
	url := c.Url + "/api/v1/namespaces/" + namespace + "/replicationcontrollers/" + replicationController

	err := c.get(url, &result)

	if err != nil {
		return &v1.ReplicationController{}, err
	}

	return &result, nil
}

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

func (c *Client) CreateReplicationController(namespace string, rc *v1.ReplicationController) (*v1.ReplicationController, error) {

	result := v1.ReplicationController{}
	url := c.Url + "/api/v1/namespaces/" + namespace + "/replicationcontrollers"
	err := c.post(url, &rc, &result)

	if err != nil {
		return &v1.ReplicationController{}, err
	}

	return &result, nil
}

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

func (c *Client) ListServices(namespace string) (*v1.ServiceList, error) {
	result := v1.ServiceList{}
	url := c.Url + "/api/v1/namespaces/" + namespace + "/services"

	err := c.get(url, &result)

	if err != nil {
		return &v1.ServiceList{}, err
	}

	return &result, nil
}

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

func (c *Client) GetService(namespace, service string) (*v1.Service, error) {
	result := v1.Service{}
	url := c.Url + "/api/v1/namespaces/" + namespace + "/services/" + service

	err := c.get(url, &result)

	if err != nil {
		return &v1.Service{}, err
	}

	return &result, nil
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
