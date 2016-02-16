package cluster

import (
	etcdclient "com.amdatu.rti.deployment/Godeps/_workspace/src/github.com/coreos/etcd/client"
	"com.amdatu.rti.deployment/Godeps/_workspace/src/k8s.io/kubernetes/pkg/api"
	"com.amdatu.rti.deployment/Godeps/_workspace/src/k8s.io/kubernetes/pkg/client/unversioned"
	"com.amdatu.rti.deployment/Godeps/_workspace/src/k8s.io/kubernetes/pkg/fields"
	"com.amdatu.rti.deployment/Godeps/_workspace/src/k8s.io/kubernetes/pkg/labels"
	"com.amdatu.rti.deployment/healthcheck"
	"com.amdatu.rti.deployment/proxies"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"
	"strings"
	"com.amdatu.rti.deployment/Godeps/_workspace/src/github.com/coreos/etcd/client"
	"bytes"
	"regexp"
	"strconv"
)

type Deployment struct {
	DeploymentType string      `json:"deploymentType,omitempty"`
	NewVersion     string      `json:"newVersion,omitempty"`
	AppName        string      `json:"appName,omitempty"`
	Replicas       int         `json:"replicas,omitempty"`
	Frontend       string      `json:"frontend,omitempty"`
	ProxyPorts	   []int	   `json:"proxyports:omitempty"`
	PodSpec        api.PodSpec `json:podspec`
	UseHealthCheck bool        `json:"useHealthCheck,omitempty"`
	Namespace      string      `json:"namespace,omitempty"`
	Email          string      `json:"email,omitempty`
	Password       string      `json:"password,omitempty`
	HealthCheckUrl string	   `json:healthcheckUrl,omitempty`
	Kafka 		   string      `json:kafka`
	InfluxDbUrl    string      `json:influxdbUrl`
	InfluxDbUser   string      `json:influxdbUser`
	InfluxDbUPassword string   `json:influxdbPassword`
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

func (deployment *Deployment) SetDefaults() *Deployment{
	if len(deployment.Namespace) == 0 {
		deployment.Namespace = api.NamespaceDefault
	}

	if len(deployment.DeploymentType) == 0 {
		deployment.DeploymentType = "blue-green"
	}

	for _,container := range deployment.PodSpec.Containers {
		if container.ImagePullPolicy == "" {
			container.ImagePullPolicy = "Always"
		}
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

	for i,container := range deployment.PodSpec.Containers {
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
	K8client          *unversioned.Client
	Logger            Logger
	ProxyConfigurator *proxies.ProxyConfigurator
	EtcdClient *client.Client
}

type Logger interface {
	Println(v ...interface{})
	Printf(format string, v ...interface{})
	Flush()
}


func NewDeployer(kubernetesUrl string, kubernetesUsername string, kubernetesPassword string, etcdUrl string, deployment Deployment, logger Logger) *Deployer {

	config := unversioned.Config{Host: kubernetesUrl, Version: "v1", Username: kubernetesUsername, Password: kubernetesPassword, Insecure: true}
	c, err := unversioned.New(&config)

	if err != nil {
		log.Panic("Error creating Kuberentes client", err)
	}

	logger.Printf("Connected to Kubernetes API server on %v\n", kubernetesUrl)
	logger.Printf("Kubernetes version %v\n", c.APIVersion())

	cfg := etcdclient.Config{
		Endpoints: []string{etcdUrl},
	}

	etcdClient, err := etcdclient.New(cfg)
	if err != nil {
		log.Fatal("Couldn't connect to etcd")
	}

	return &Deployer{kubernetesUrl, deployment, etcdUrl, c, logger, proxies.NewProxyConfigurator(etcdClient), &etcdClient}

}

func (deployer *Deployer) CreateRcName() string {
	return deployer.Deployment.AppName + "-" + deployer.Deployment.NewVersion
}

func (deployer *Deployer) CreateReplicationController() (*api.ReplicationController, error) {

	ctrl := new(api.ReplicationController)
	rcName := deployer.CreateRcName()
	ctrl.Name = rcName

	labels := make(map[string]string)
	labels["name"] = rcName
	labels["version"] = deployer.Deployment.NewVersion
	labels["app"] = deployer.Deployment.AppName

	ctrl.Labels = labels

	containers := []api.Container{}

	for _, container := range deployer.Deployment.PodSpec.Containers {
		fmt.Println("Setting env vars on container")
		container.Env = append(container.Env,
			api.EnvVar{Name: "APP_NAME", Value: deployer.Deployment.AppName},
			api.EnvVar{Name: "POD_NAMESPACE", Value: deployer.Deployment.Namespace},
			api.EnvVar{Name: "APP_VERSION", Value: deployer.Deployment.NewVersion},
			api.EnvVar{Name: "KAFKA", Value: deployer.Deployment.Kafka},
			api.EnvVar{Name: "INFLUX_URL", Value: deployer.Deployment.InfluxDbUrl},
			api.EnvVar{Name: "INFLUX_USERNAME", Value: deployer.Deployment.InfluxDbUser},
			api.EnvVar{Name: "INFLUX_PASSWORD", Value: deployer.Deployment.InfluxDbUPassword},
			api.EnvVar{Name: "POD_NAME", ValueFrom: &api.EnvVarSource{FieldRef: &api.ObjectFieldSelector{FieldPath: "metadata.name"}}})


		containers = append(containers, container)
	}

	deployer.Deployment.PodSpec.Containers = containers


	bytes,_ := json.MarshalIndent(deployer.Deployment.PodSpec, "", "  ")
	fmt.Printf("%v", string(bytes))

	ctrl.Spec = api.ReplicationControllerSpec{
		Selector: map[string]string{
			"name":    rcName,
			"version": deployer.Deployment.NewVersion,
			"app":     deployer.Deployment.AppName,
		},
		Replicas: deployer.Deployment.Replicas,
		Template: &api.PodTemplateSpec{
			ObjectMeta: api.ObjectMeta{
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
	var result, err = deployer.K8client.ReplicationControllers(deployer.Deployment.Namespace).Create(ctrl)
	if err != nil {
		deployer.Logger.Println("Error while creating replication controller")

		return result, err
	}

	deployer.Logger.Printf("Replication Controller %v created\n", result.ObjectMeta.Name)

	return result, nil

}

func (deployer *Deployer) CreateService() (*api.Service, error) {
	srv := new(api.Service)
	srv.Name = deployer.CreateRcName()

	selector := make(map[string]string)
	selector["name"] = deployer.CreateRcName()
	selector["version"] = deployer.Deployment.NewVersion
	selector["app"] = deployer.Deployment.AppName

	srv.Labels = selector

	ports := []api.ServicePort{}
	for _,container := range deployer.Deployment.PodSpec.Containers {
		for _,port := range container.Ports {

			servicePort := api.ServicePort{Port: port.ContainerPort}
			if port.Name != "" {
				servicePort.Name = port.Name
			}
			ports = append(ports, servicePort)
		}
	}

	srv.Spec = api.ServiceSpec{
		Selector: selector,
		Ports: ports,
		Type: api.ServiceTypeNodePort,
		SessionAffinity: "ClientIP",
	}

	deployer.Logger.Println("Creating Service")

	return deployer.K8client.Services(deployer.Deployment.Namespace).Create(srv)
}

func (deployer *Deployer) FindCurrentRc() ([]api.ReplicationController, error) {
	result := []api.ReplicationController{}

	rcLabelSelector := labels.Set{"app": deployer.Deployment.AppName}.AsSelector()
	replicationControllers, _ := deployer.K8client.ReplicationControllers(deployer.Deployment.Namespace).List(rcLabelSelector)

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

func (deployer *Deployer) FindCurrentPods(allowSameVersion bool) ([]api.Pod, error) {
	result := make([]api.Pod, 0, 10)

	rcLabelSelector := labels.Set{"app": deployer.Deployment.AppName}.AsSelector()
	pods, _ := deployer.K8client.Pods(deployer.Deployment.Namespace).List(rcLabelSelector, fields.Everything())

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

func (deployer *Deployer) FindCurrentService() ([]api.Service, error) {
	result := make([]api.Service, 1, 10)

	rcLabelSelector := labels.Set{"app": deployer.Deployment.AppName}.AsSelector()
	services, _ := deployer.K8client.Services(deployer.Deployment.Namespace).List(rcLabelSelector)

	for _, service := range services.Items {
		if service.Labels["version"] != deployer.Deployment.NewVersion {

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

func (deployer *Deployer) deleteRc(rc api.ReplicationController) {
	deployer.Logger.Printf("Deleting RC %v", rc.Name)

	deployer.K8client.ReplicationControllers(deployer.Deployment.Namespace).Delete(rc.Name)
}

func (deployer *Deployer) DeletePod(pod api.Pod) {
	deployer.Logger.Printf("Deleting Pod %v", pod.Name)

	deployer.K8client.Pods(deployer.Deployment.Namespace).Delete(pod.Name, &api.DeleteOptions{})
}

func (deployer *Deployer) deleteService(service api.Service) {
	deployer.Logger.Printf("Deleting Service %v", service.Name)
	deployer.K8client.Services(deployer.Deployment.Namespace).Delete(service.Name)
}

func (deployer *Deployer) CountRunningPods(pods []api.Pod) int {
	nrOfRunning := 0

	for _, pod := range pods {
		if pod.Status.Phase == "Running" {
			nrOfRunning++
		}
	}

	return nrOfRunning
}

func (deployer *Deployer) CheckPodHealth(pod *api.Pod) error {
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

func FindHealthcheckPort(pod *api.Pod) int {

	ports := pod.Spec.Containers[0].Ports


	if(ports == nil || len(ports) == 0) {
		//If no ports are defined, 9999 is the default
		return 9999
	} else if(len(ports) == 1) {
		//If one port is defined, it must be the health check port
		return ports[0].ContainerPort
	} else {
		//If multiple ports are defined, check for a named port "healthcheck"
		for _, port :=range ports {
			if port.Name == "healthcheck" {
				return port.ContainerPort
			}
		}

		//If no named healthcheck port if found, assume the first port
		return ports[0].ContainerPort
	}

	return 0
}

func (deployer *Deployer) getHealthcheckUrl(host string, port int) string{
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

func (deployer *Deployer) findRcForDeployment() (*api.ReplicationController, error){
	return deployer.K8client.ReplicationControllers(deployer.Deployment.Namespace).Get(deployer.CreateRcName())
}

func (deployer *Deployer) findServiceForDeployment() (*api.Service, error){
	return deployer.K8client.Services(deployer.Deployment.Namespace).Get(deployer.CreateRcName())
}

func (deployer *Deployer) findPodsForDeployment() (*api.PodList, error){
	rcLabelSelector := labels.Set{"app": deployer.Deployment.AppName, "version": deployer.Deployment.NewVersion}.AsSelector()
	return deployer.K8client.Pods(deployer.Deployment.Namespace).List(rcLabelSelector, fields.Everything())
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
		for _,pod := range pods.Items {
			deployer.Logger.Printf("Deleting pod %v\n", pod.Name)
			deployer.DeletePod(pod)
		}
	}

	deployer.Logger.Printf("Deleting proxy config %v\n", rc.Namespace + "-" + rc.Name)
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