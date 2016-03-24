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
package cluster

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/healthcheck"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/proxies"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-go/api/util"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-go/api/v1"
	k8sClient "bitbucket.org/amdatulabs/amdatu-kubernetes-go/client"
	etcdclient "github.com/coreos/etcd/client"
)

type Deployment struct {
	Id             string            `json:"id,omitempty"`
	WebHooks       []WebHook         `json:"webhooks,omitempty"`
	DeploymentType string            `json:"deploymentType,omitempty"`
	NewVersion     string            `json:"newVersion,omitempty"`
	AppName        string            `json:"appName,omitempty"`
	Replicas       int               `json:"replicas,omitempty"`
	Frontend       string            `json:"frontend,omitempty"`
	ProxyPorts     []int             `json:"proxyports,omitempty"`
	PodSpec        v1.PodSpec        `json:"podspec,omitempty"`
	UseHealthCheck bool              `json:"useHealthCheck,omitempty"`
	Namespace      string            `json:"namespace,omitempty"`
	Email          string            `json:"email,omitempty"`
	Password       string            `json:"password,omitempty"`
	HealthCheckUrl string            `json:"healthcheckUrl,omitempty"`
	Environment    map[string]string `json:"environment,omitempty"`
}

type DeploymentResult struct {
	Date       string     `json:"date,omitempty"`
	Status     string     `json:"status,omitempty"`
	Deployment Deployment `json:"deployment,omitempty"`
}

type DeploymentHistory struct {
	Id                string             `json:"id,omitempty"`
	Namespace         string             `json:"namespace,omitempty"`
	AppName           string             `json:"appName,omitempty"`
	DeploymentResults []DeploymentResult `json:"deploymentResults,omitempty"`
}

type WebHook struct {
	Description string `json:"description,omitempty"`
	Key         string `json:"key,omitempty"`
}

const DNS952LabelFmt string = "[a-z]([-a-z0-9]*[a-z0-9])?"

var dns952LabelRegexp = regexp.MustCompile("^" + DNS952LabelFmt + "$")

func (deployment *Deployment) String() string {
	b, err := json.MarshalIndent(deployment, "", "    ")

	if err != nil {
		return "Error writing deployment to JSON"
	}

	return string(b)
}

func (deployment *Deployment) SetDefaults() *Deployment {

	if len(deployment.Namespace) == 0 {
		deployment.Namespace = v1.NamespaceDefault
	}

	if len(deployment.DeploymentType) == 0 {
		deployment.DeploymentType = "blue-green"
	}

	if len(deployment.PodSpec.RestartPolicy) == 0 {
		deployment.PodSpec.RestartPolicy = v1.RestartPolicyAlways
	}
	if len(deployment.PodSpec.DNSPolicy) == 0 {
		deployment.PodSpec.DNSPolicy = v1.DNSClusterFirst
	}

	for i := range deployment.PodSpec.Containers {
		container := deployment.PodSpec.Containers[i]
		if len(container.ImagePullPolicy) == 0 {
			container.ImagePullPolicy = v1.PullAlways
		}

		for j := range container.Ports {
			if len(container.Ports[j].Protocol) == 0 {
				container.Ports[j].Protocol = v1.ProtocolTCP
			}
		}
		deployment.PodSpec.Containers[i] = container
	}

	deployment.AppName = strings.Replace(deployment.AppName, ".", "-", -1)
	deployment.AppName = strings.Replace(deployment.AppName, "_", "-", -1)

	deployment.NewVersion = strings.Replace(deployment.NewVersion, ".", "-", -1)
	deployment.NewVersion = strings.Replace(deployment.NewVersion, "_", "-", -1)

	deployment.AppName = strings.ToLower(deployment.AppName)

	if strings.ToLower(deployment.NewVersion) == "#" {
		//Make sure to pass validation, but assume a version of 3 characters. Value will be replaced later
		deployment.NewVersion = "000"
	}

	deployment.NewVersion = strings.ToLower(deployment.NewVersion)

	return deployment
}

func (deployment *Deployment) Validate() error {

	var messageBuffer bytes.Buffer

	//Currently only blue-green deployments are supported
	if deployment.DeploymentType != "blue-green" {
		messageBuffer.WriteString(fmt.Sprintf("Unsupported deploymentType '%v'\n", deployment.DeploymentType))
	}

	if deployment.AppName == "" {
		messageBuffer.WriteString("Missing required property 'appName'\n")
	}

	if deployment.Namespace == "" {
		messageBuffer.WriteString("Missing required property 'namespace'\n")
	}

	if deployment.NewVersion == "" {
		messageBuffer.WriteString("Missing required property 'newVersion'\n")
	}

	if len(deployment.PodSpec.Containers) == 0 {
		messageBuffer.WriteString("No containers specified in PodSpec\n")
	}

	for i, container := range deployment.PodSpec.Containers {
		if container.Image == "" {
			messageBuffer.WriteString(fmt.Sprintf("No image specified for container %v\n", i))
		}
	}

	appName := deployment.AppName + "-" + deployment.NewVersion
	if len(appName) > 24 {
		messageBuffer.WriteString(fmt.Sprintf("Application name %v is too long. A maximum of 24 characters is allowed\n", appName))
	}

	if !dns952LabelRegexp.MatchString(appName) {
		messageBuffer.WriteString(fmt.Sprintf("Application name %v doesn't match pattern [a-z]([-a-z0-9]*[a-z0-9])?\n", appName))
	}

	message := messageBuffer.String()

	if len(message) > 0 {
		return errors.New(message)
	}

	return nil
}

type Deployer struct {
	KubernetesUrl     string
	Deployment        Deployment
	EtcdUrl           string
	K8client          *k8sClient.Client
	Logger            Logger
	ProxyConfigurator *proxies.ProxyConfigurator
	EtcdClient        *etcdclient.Client
}

type Logger interface {
	Println(v ...interface{})
	Printf(format string, v ...interface{})
	Flush()
}

func NewDeployer(kubernetesUrl string, kubernetesUsername string, kubernetesPassword string, etcdUrl string, deployment Deployment, logger Logger) *Deployer {

	c := k8sClient.NewClient(kubernetesUrl, kubernetesUsername, kubernetesPassword)
	logger.Printf("Connected to Kubernetes API server on %v\n", kubernetesUrl)

	cfg := etcdclient.Config{
		Endpoints: []string{etcdUrl},
	}

	etcdClient, err := etcdclient.New(cfg)
	if err != nil {
		log.Fatal("Couldn't connect to etcd")
	}

	return &Deployer{kubernetesUrl, deployment, etcdUrl, &c, logger, proxies.NewProxyConfigurator(etcdClient), &etcdClient}

}

func (deployer *Deployer) CreateRcName() string {
	return deployer.Deployment.AppName + "-" + deployer.Deployment.NewVersion
}

func (deployer *Deployer) CreateReplicationController() (*v1.ReplicationController, error) {

	ctrl := new(v1.ReplicationController)
	rcName := deployer.CreateRcName()
	ctrl.Name = rcName

	labels := make(map[string]string)
	labels["name"] = rcName
	labels["version"] = deployer.Deployment.NewVersion
	labels["app"] = deployer.Deployment.AppName

	ctrl.Labels = labels

	containers := []v1.Container{}

	for _, container := range deployer.Deployment.PodSpec.Containers {
		fmt.Println("Setting env vars on container")
		container.Env = append(container.Env,
			v1.EnvVar{Name: "APP_NAME", Value: deployer.Deployment.AppName},
			v1.EnvVar{Name: "POD_NAMESPACE", Value: deployer.Deployment.Namespace},
			v1.EnvVar{Name: "APP_VERSION", Value: deployer.Deployment.NewVersion},
			v1.EnvVar{Name: "POD_NAME", ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.name"}}})

		for key, val := range deployer.Deployment.Environment {
			container.Env = append(container.Env, v1.EnvVar{Name: key, Value: val})
		}

		containers = append(containers, container)
	}

	deployer.Deployment.PodSpec.Containers = containers

	bytes, _ := json.MarshalIndent(deployer.Deployment.PodSpec, "", "  ")
	fmt.Printf("%v", string(bytes))

	replicas := int32(deployer.Deployment.Replicas)

	ctrl.Spec = v1.ReplicationControllerSpec{
		Selector: map[string]string{
			"name":    rcName,
			"version": deployer.Deployment.NewVersion,
			"app":     deployer.Deployment.AppName,
		},
		Replicas: &replicas,
		Template: &v1.PodTemplateSpec{
			ObjectMeta: v1.ObjectMeta{
				Labels: map[string]string{
					"name":    rcName,
					"version": deployer.Deployment.NewVersion,
					"app":     deployer.Deployment.AppName,
				},
			},
			Spec: deployer.Deployment.PodSpec,
		},
	}

	deployer.Logger.Println("Creating Replication Controller")
	var result, err = deployer.K8client.CreateReplicationController(deployer.Deployment.Namespace, ctrl)
	if err != nil {
		deployer.Logger.Println("Error while creating replication controller")

		return result, err
	}

	deployer.Logger.Printf("Replication Controller %v created\n", result.ObjectMeta.Name)

	return result, nil

}

func (deployer *Deployer) CreateService() (*v1.Service, error) {
	srv := new(v1.Service)
	srv.Name = deployer.CreateRcName()

	selector := make(map[string]string)
	selector["name"] = deployer.CreateRcName()
	selector["version"] = deployer.Deployment.NewVersion
	selector["app"] = deployer.Deployment.AppName

	srv.Labels = selector

	ports := []v1.ServicePort{}
	for _, container := range deployer.Deployment.PodSpec.Containers {
		for _, port := range container.Ports {

			servicePort := v1.ServicePort{Port: port.ContainerPort}
			if port.Name != "" {
				servicePort.Name = port.Name
				servicePort.TargetPort = intstr.IntOrString{Type: intstr.String, StrVal: port.Name}
			} else {
				servicePort.TargetPort = intstr.IntOrString{Type: intstr.Int, IntVal: port.ContainerPort}
			}
			servicePort.Protocol = v1.ProtocolTCP
			ports = append(ports, servicePort)
		}
	}

	srv.Spec = v1.ServiceSpec{
		Selector:        selector,
		Ports:           ports,
		Type:            v1.ServiceTypeNodePort,
		SessionAffinity: "ClientIP",
	}

	deployer.Logger.Println("Creating Service")

	return deployer.K8client.CreateService(deployer.Deployment.Namespace, srv)
}

func (deployer *Deployer) CreatePersistentService() (*v1.Service, error) {
	existing, _ := deployer.K8client.GetService(deployer.Deployment.Namespace, deployer.Deployment.AppName)
	if existing.Name == "" {
		srv := new(v1.Service)
		srv.Name = deployer.Deployment.AppName

		labels := make(map[string]string)
		labels["app"] = deployer.Deployment.AppName
		labels["name"] = deployer.Deployment.AppName
		labels["persistent"] = "true"

		srv.Labels = labels

		ports := []v1.ServicePort{}
		for _, container := range deployer.Deployment.PodSpec.Containers {
			for _, port := range container.Ports {

				servicePort := v1.ServicePort{Port: port.ContainerPort}
				if port.Name != "" {
					servicePort.Name = port.Name
					servicePort.TargetPort = intstr.IntOrString{Type: intstr.String, StrVal: port.Name}
				} else {
					servicePort.TargetPort = intstr.IntOrString{Type: intstr.Int, IntVal: port.ContainerPort}
				}
				servicePort.Protocol = v1.ProtocolTCP
				ports = append(ports, servicePort)
			}
		}

		selector := make(map[string]string)
		selector["app"] = deployer.Deployment.AppName

		srv.Spec = v1.ServiceSpec{
			Selector:        selector,
			Ports:           ports,
			Type:            v1.ServiceTypeNodePort,
			SessionAffinity: "ClientIP",
		}

		created, err := deployer.K8client.CreateService(deployer.Deployment.Namespace, srv)

		if err == nil {
			deployer.Logger.Printf("Creating persistent service %v. Listening on IP %v\n", srv.Name, created.Spec.ClusterIP)
		}
		return created, err

	} else {
		deployer.Logger.Printf("Persistent service %v already exists on IP %v\n", existing.Name, existing.Spec.ClusterIP)
		return existing, nil
	}
}

func (deployer *Deployer) FindCurrentRc() ([]v1.ReplicationController, error) {
	result := []v1.ReplicationController{}

	labels := map[string]string{"app": deployer.Deployment.AppName}
	replicationControllers, _ := deployer.K8client.ListReplicationControllersWithLabel(deployer.Deployment.Namespace, labels)

	for _, rc := range replicationControllers.Items {
		if rc.Labels["version"] != deployer.Deployment.NewVersion {

			result = append(result, rc)
		}
	}

	if len(result) == 0 {
		return result, errors.New("No active Replica Controller found")
	} else {
		return result, nil
	}
}

func (deployer *Deployer) FindCurrentPods(allowSameVersion bool) ([]v1.Pod, error) {
	result := make([]v1.Pod, 0, 10)

	labels := map[string]string{"app": deployer.Deployment.AppName}
	pods, _ := deployer.K8client.ListPodsWithLabel(deployer.Deployment.Namespace, labels)

	for _, rc := range pods.Items {
		if allowSameVersion || rc.Labels["version"] != deployer.Deployment.NewVersion {

			result = append(result, rc)
		}
	}

	if len(result) == 0 {
		return result, errors.New("No active Pods found")
	} else {
		return result, nil
	}
}

func (deployer *Deployer) FindCurrentService() ([]v1.Service, error) {
	result := make([]v1.Service, 1, 10)

	labels := map[string]string{"app": deployer.Deployment.AppName}
	services, err := deployer.K8client.ListServicesWithLabel(deployer.Deployment.Namespace, labels)

	if err != nil {
		return result, errors.New("No active Service found")
	}

	for _, service := range services.Items {
		if service.Labels["version"] != deployer.Deployment.NewVersion && service.Labels["persistent"] != "true" {

			result = append(result, service)
		}
	}

	if len(result) == 0 {
		return result, errors.New("No active Service found")
	} else {
		return result, nil
	}
}

func (deployer *Deployer) CleaupOldDeployments() {
	controllers, err := deployer.FindCurrentRc()

	if err != nil {
		deployer.Logger.Println("Did not find a old Replication Controller to remove")
		return
	}

	for _, rc := range controllers {
		if rc.Name != "" {
			deployer.deleteRc(rc)
			deployer.ProxyConfigurator.DeleteDeployment(rc.Namespace + "-" + rc.Name)
		}
	}

	log.Println("Looking for old pods...")
	pods, err := deployer.FindCurrentPods(false)

	if err != nil {
		deployer.Logger.Println("Did not find old pods to remove")
	}

	for _, pod := range pods {
		if pod.Name != "" {
			deployer.DeletePod(pod)
		}
	}

	log.Println("Looking for services...")
	services, err := deployer.FindCurrentService()

	if err != nil {
		deployer.Logger.Println("Did not find a old Replication Controller to remove")
		return
	}

	for _, service := range services {
		if service.Name != "" {
			deployer.deleteService(service)
		}
	}
}

func (deployer *Deployer) deleteRc(rc v1.ReplicationController) {
	deployer.Logger.Printf("Deleting RC %v", rc.Name)

	deployer.K8client.DeleteReplicationController(deployer.Deployment.Namespace, rc.Name)
}

func (deployer *Deployer) DeletePod(pod v1.Pod) {
	deployer.Logger.Printf("Deleting Pod %v", pod.Name)

	deployer.K8client.DeletePod(deployer.Deployment.Namespace, pod.Name)
}

func (deployer *Deployer) deleteService(service v1.Service) {
	deployer.Logger.Printf("Deleting Service %v", service.Name)
	deployer.K8client.DeleteService(deployer.Deployment.Namespace, service.Name)
}

func (deployer *Deployer) CountRunningPods(pods []v1.Pod) int {
	nrOfRunning := 0

	for _, pod := range pods {
		if pod.Status.Phase == "Running" {
			nrOfRunning++
		}
	}

	return nrOfRunning
}

func (deployer *Deployer) CheckPodHealth(pod *v1.Pod) error {
	if deployer.Deployment.UseHealthCheck {

		port := FindHealthcheckPort(pod)
		host := pod.Status.PodIP
		healthy := healthcheck.WaitForPodStarted(deployer.getHealthcheckUrl(host, port), time.Minute*2)
		if !healthy {
			return errors.New("Pod didn't get healthy")
		}
	}

	return nil
}

func FindHealthcheckPort(pod *v1.Pod) int32 {

	ports := pod.Spec.Containers[0].Ports

	if ports == nil || len(ports) == 0 {
		//If no ports are defined, 9999 is the default
		return 9999
	} else if len(ports) == 1 {
		//If one port is defined, it must be the health check port
		return ports[0].ContainerPort
	} else {
		//If multiple ports are defined, check for a named port "healthcheck"
		for _, port := range ports {
			if port.Name == "healthcheck" {
				return port.ContainerPort
			}
		}

		//If no named healthcheck port if found, assume the first port
		return ports[0].ContainerPort
	}
}

func (deployer *Deployer) getHealthcheckUrl(host string, port int32) string {
	var healthUrl string
	if deployer.Deployment.HealthCheckUrl != "" {
		if strings.HasPrefix(deployer.Deployment.HealthCheckUrl, "/") {
			healthUrl = strings.TrimPrefix(deployer.Deployment.HealthCheckUrl, "/")
		} else {
			healthUrl = deployer.Deployment.HealthCheckUrl
		}
	} else {
		healthUrl = "health"
	}

	return fmt.Sprintf("http://%v:%v/%v", host, port, healthUrl)
}

func (deployer *Deployer) findRcForDeployment() (*v1.ReplicationController, error) {
	return deployer.K8client.GetReplicationController(deployer.Deployment.Namespace, deployer.CreateRcName())
}

func (deployer *Deployer) findServiceForDeployment() (*v1.Service, error) {
	return deployer.K8client.GetService(deployer.Deployment.Namespace, deployer.CreateRcName())
}

func (deployer *Deployer) findPodsForDeployment() (*v1.PodList, error) {
	rcLabelSelector := map[string]string{"app": deployer.Deployment.AppName, "version": deployer.Deployment.NewVersion}
	return deployer.K8client.ListPodsWithLabel(deployer.Deployment.Namespace, rcLabelSelector)
}

func (deployer *Deployer) CleanupFailedDeployment() {
	deployer.Logger.Println("Cleaning up resources created by deployment")

	rc, err := deployer.findRcForDeployment()

	if err == nil {
		deployer.Logger.Printf("Deleting ReplicationController %v\n", rc.Name)
		deployer.deleteRc(*rc)
	}

	pods, err := deployer.findPodsForDeployment()
	if err == nil {
		for _, pod := range pods.Items {
			deployer.Logger.Printf("Deleting pod %v\n", pod.Name)
			deployer.DeletePod(pod)
		}
	}

	deployer.Logger.Printf("Deleting proxy config %v\n", rc.Namespace+"-"+rc.Name)
	deployer.ProxyConfigurator.DeleteDeployment(rc.Namespace + "-" + rc.Name)
	service, err := deployer.findServiceForDeployment()

	if err == nil {
		deployer.Logger.Printf("Deleting Service %v\n", service.Name)
		deployer.deleteService(*service)
	}
}

func DetermineNewVersion(oldVersion string) (string, error) {
	version, err := strconv.Atoi(oldVersion)
	if err != nil {
		return "", err
	} else {
		return strconv.Itoa(version + 1), nil
	}
}
