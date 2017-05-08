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
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/helper"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/k8s"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/pkg/api/v1"
)

const DNS952LabelFmt string = "[a-z]([-a-z0-9]*[a-z0-9])?"

var dns952LabelRegexp = regexp.MustCompile("^" + DNS952LabelFmt + "$")

type Deployer struct {
	Config     *helper.DeployerConfig
	Deployment *types.Deployment
	K8client   k8s.K8sClient
	Logger     logger.Logger
}

func NewDeployer(config helper.DeployerConfig, deployment *types.Deployment, logger logger.Logger) *Deployer {

	return &Deployer{&config, deployment, k8s.New(config.K8sUrl, logger), logger}

}

func (deployer *Deployer) CreateRcName() string {
	return deployer.Deployment.Descriptor.AppName + "-" + deployer.Deployment.Version
}

func (deployer *Deployer) CreateReplicationController() (*v1.ReplicationController, error) {

	descriptor := deployer.Deployment.Descriptor

	ctrl := new(v1.ReplicationController)
	rcName := deployer.CreateRcName()
	ctrl.Name = rcName

	labels := make(map[string]string)
	labels["name"] = rcName
	labels["version"] = deployer.Deployment.Version
	labels["app"] = descriptor.AppName

	ctrl.Labels = labels

	annotations := make(map[string]string)
	annotations["deploymentTs"] = deployer.Deployment.Created
	annotations["deploymentId"] = deployer.Deployment.Id
	annotations["appName"] = descriptor.AppName
	annotations["version"] = deployer.Deployment.Version
	annotations["useHealthCheck"] = strconv.FormatBool(descriptor.UseHealthCheck)
	annotations["healthCheckPath"] = descriptor.HealthCheckPath
	annotations["healthCheckPort"] = strconv.Itoa(descriptor.HealthCheckPort)
	annotations["healthCheckType"] = descriptor.HealthCheckType
	annotations["frontend"] = descriptor.Frontend

	ctrl.Annotations = annotations

	containers := []v1.Container{}

	for _, container := range descriptor.PodSpec.Containers {
		fmt.Println("Setting env vars on container")
		container.Env = append(container.Env,
			v1.EnvVar{Name: "APP_NAME", Value: descriptor.AppName},
			v1.EnvVar{Name: "POD_NAMESPACE", Value: descriptor.Namespace},
			v1.EnvVar{Name: "APP_VERSION", Value: deployer.Deployment.Version},
			v1.EnvVar{Name: "POD_NAME", ValueFrom: &v1.EnvVarSource{FieldRef: &v1.ObjectFieldSelector{APIVersion: "v1", FieldPath: "metadata.name"}}})

		// ATTENTION: if you add more EnvVars here, also remove them in etcdregistry.go/cleanDescriptor() !

		for key, val := range descriptor.Environment {
			container.Env = append(container.Env, v1.EnvVar{Name: key, Value: val})
		}

		containers = append(containers, container)
	}

	descriptor.PodSpec.Containers = containers

	bytes, _ := json.MarshalIndent(descriptor.PodSpec, "", "  ")
	fmt.Printf("%v", string(bytes))

	replicas := int32(descriptor.Replicas)

	ctrl.Spec = v1.ReplicationControllerSpec{
		Selector: map[string]string{
			"name":    rcName,
			"version": deployer.Deployment.Version,
			"app":     descriptor.AppName,
		},
		Replicas: &replicas,
		Template: &v1.PodTemplateSpec{
			ObjectMeta: meta.ObjectMeta{
				Labels: map[string]string{
					"name":    rcName,
					"version": deployer.Deployment.Version,
					"app":     descriptor.AppName,
				},
			},
			Spec: descriptor.PodSpec,
		},
	}

	deployer.Logger.Println("Creating Replication Controller")
	result, err := deployer.K8client.CreateReplicationController(descriptor.Namespace, ctrl)
	if err != nil {
		deployer.Logger.Println("Error while creating replication controller")
		return result, err
	}

	deployer.Logger.Printf("Replication Controller %v created\n", result.ObjectMeta.Name)
	return result, nil

}

func (deployer *Deployer) CreateService() (*v1.Service, error) {

	descriptor := deployer.Deployment.Descriptor

	srv := new(v1.Service)
	srv.Name = deployer.CreateRcName()

	selector := make(map[string]string)
	selector["name"] = deployer.CreateRcName()
	selector["version"] = deployer.Deployment.Version
	selector["app"] = descriptor.AppName

	srv.Labels = selector

	ports := []v1.ServicePort{}
	for _, container := range descriptor.PodSpec.Containers {
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
		SessionAffinity: "None",
	}

	deployer.Logger.Println("Creating Service")

	return deployer.K8client.CreateService(descriptor.Namespace, srv)
}

func (deployer *Deployer) CreateOrUpdatePersistentService() (*v1.Service, error) {

	deployment := deployer.Deployment
	descriptor := deployment.Descriptor

	existing, _ := deployer.K8client.GetService(descriptor.Namespace, descriptor.AppName)
	if existing.Name == "" {
		srv := new(v1.Service)
		srv.Name = descriptor.AppName

		labels := make(map[string]string)
		labels["app"] = descriptor.AppName
		labels["name"] = descriptor.AppName
		labels["persistent"] = "true"

		srv.Labels = labels

		ports := getPorts(descriptor.PodSpec.Containers)

		selector := make(map[string]string)
		selector["app"] = descriptor.AppName
		selector["version"] = deployment.Version

		srv.Spec = v1.ServiceSpec{
			Selector:        selector,
			Ports:           ports,
			Type:            v1.ServiceTypeNodePort,
			SessionAffinity: "None",
		}

		created, err := deployer.K8client.CreateService(descriptor.Namespace, srv)

		if err == nil {
			deployer.Logger.Printf("Creating persistent service %v. Listening on IP %v\n", srv.Name, created.Spec.ClusterIP)
		}
		return created, err

	} else {
		deployer.Logger.Printf("Persistent service %v already exists on IP %v\n", existing.Name, existing.Spec.ClusterIP)

		// check if ports changed
		newPorts := getPorts(descriptor.PodSpec.Containers)
		existingPorts := existing.Spec.Ports
		if !servicePortEquals(newPorts, existingPorts) {
			deployer.Logger.Println("Updating persistent service ports")
			existing.Spec.Ports = newPorts
		}

		deployer.Logger.Println("Updating persistent service version selector")
		existing.Spec.Selector["version"] = deployment.Version

		// update session affinity
		existing.Spec.SessionAffinity = "None"

		_, err := deployer.K8client.UpdateService(descriptor.Namespace, existing)
		if err != nil {
			deployer.Logger.Println("Error updating persistent service: " + err.Error())
			return existing, err
		}

		return existing, nil
	}
}

func getPorts(containers []v1.Container) []v1.ServicePort {
	ports := []v1.ServicePort{}
	for _, container := range containers {
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
	return ports
}

func servicePortEquals(a, b []v1.ServicePort) bool {
	if len(a) != len(b) {
		return false
	}
	for i, port := range a {
		otherPort := b[i]
		if port.Port != otherPort.Port ||
			port.TargetPort != otherPort.TargetPort ||
			port.Protocol != otherPort.Protocol {

			return false
		}
	}
	return true
}

func (deployer *Deployer) FindCurrentRc() ([]v1.ReplicationController, error) {

	descriptor := deployer.Deployment.Descriptor

	result := []v1.ReplicationController{}

	selector := map[string]string{"app": descriptor.AppName}
	replicationControllers, err := deployer.K8client.ListReplicationControllersWithSelector(descriptor.Namespace, selector)
	if err != nil {
		return nil, err
	}

	for _, rc := range replicationControllers.Items {
		if rc.Labels["version"] != deployer.Deployment.Version {
			result = append(result, rc)
		}
	}

	return result, nil
}

func (deployer *Deployer) FindCurrentPods(allowSameVersion bool) ([]v1.Pod, error) {

	descriptor := deployer.Deployment.Descriptor

	result := make([]v1.Pod, 0, 10)

	selector := map[string]string{"app": descriptor.AppName}
	pods, err := deployer.K8client.ListPodsWithSelector(descriptor.Namespace, selector)
	if err != nil {
		return result, err
	}

	for _, pod := range pods.Items {
		if allowSameVersion || pod.Labels["version"] != deployer.Deployment.Version {
			result = append(result, pod)
		}
	}

	if len(result) == 0 {
		return result, errors.New("No active Pods found")
	} else {
		return result, nil
	}
}

func (deployer *Deployer) FindCurrentService() ([]v1.Service, error) {

	descriptor := deployer.Deployment.Descriptor

	result := make([]v1.Service, 1, 10)

	selector := map[string]string{"app": descriptor.AppName}
	services, err := deployer.K8client.ListServicesWithSelector(descriptor.Namespace, selector)
	if err != nil {
		return result, errors.New("No active Service found")
	}

	for _, service := range services.Items {
		if service.Labels["version"] != deployer.Deployment.Version && service.Labels["persistent"] != "true" {

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
		deployer.Logger.Printf("Error getting replication controllers: %v\n", err.Error())
		return
	}
	if len(controllers) == 0 {
		deployer.Logger.Println("Did not find a old replication controller to remove")
		return
	}

	for _, rc := range controllers {
		if rc.Name != "" {
			deployer.K8client.ShutdownReplicationController(&rc)
			deployer.Config.ProxyConfigurator.DeleteDeployment(rc.Namespace+"-"+rc.Name, deployer.Logger)
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
		deployer.Logger.Println("Did not find an old service to remove")
		return
	}

	for _, service := range services {
		if service.Name != "" {
			deployer.deleteService(service)
		}
	}
}

func (deployer *Deployer) DeletePod(pod v1.Pod) {

	descriptor := deployer.Deployment.Descriptor

	deployer.Logger.Printf("Deleting Pod %v", pod.Name)

	deployer.K8client.DeletePod(descriptor.Namespace, pod.Name)
}

func (deployer *Deployer) deleteService(service v1.Service) {
	descriptor := deployer.Deployment.Descriptor
	deployer.Logger.Printf("Deleting Service %v", service.Name)
	deployer.K8client.DeleteService(descriptor.Namespace, service.Name)
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

func (deployer *Deployer) GetHealthcheckUrl(host string, port int32) string {

	descriptor := deployer.Deployment.Descriptor

	var healthUrl string
	if descriptor.HealthCheckPath != "" {
		if strings.HasPrefix(descriptor.HealthCheckPath, "/") {
			healthUrl = strings.TrimPrefix(descriptor.HealthCheckPath, "/")
		} else {
			healthUrl = descriptor.HealthCheckPath
		}
	} else {
		healthUrl = "health"
	}

	return fmt.Sprintf("http://%v:%v/%v", host, port, healthUrl)
}

func (deployer *Deployer) findRcForDeployment() (*v1.ReplicationController, error) {
	descriptor := deployer.Deployment.Descriptor
	return deployer.K8client.GetReplicationController(descriptor.Namespace, deployer.CreateRcName())
}

func (deployer *Deployer) findServiceForDeployment() (*v1.Service, error) {
	descriptor := deployer.Deployment.Descriptor
	return deployer.K8client.GetService(descriptor.Namespace, deployer.CreateRcName())
}

func (deployer *Deployer) findPodsForDeployment() (*v1.PodList, error) {
	descriptor := deployer.Deployment.Descriptor
	selector := map[string]string{"app": descriptor.AppName, "version": deployer.Deployment.Version}
	return deployer.K8client.ListPodsWithSelector(descriptor.Namespace, selector)
}

func (deployer *Deployer) CleanupFailedDeployment() {
	deployer.Logger.Println("Cleaning up resources created by deployment")

	rc, err := deployer.findRcForDeployment()

	if err == nil {
		deployer.Logger.Printf("Deleting ReplicationController %v\n", rc.Name)
		deployer.K8client.ShutdownReplicationController(rc)
	}

	pods, err := deployer.findPodsForDeployment()
	if err == nil {
		for _, pod := range pods.Items {
			deployer.Logger.Printf("Deleting pod %v\n", pod.Name)
			deployer.DeletePod(pod)
		}
	}

	deployer.Logger.Printf("Deleting proxy config %v\n", rc.Namespace+"-"+rc.Name)
	deployer.Config.ProxyConfigurator.DeleteDeployment(rc.Namespace+"-"+rc.Name, deployer.Logger)
	service, err := deployer.findServiceForDeployment()

	if err == nil {
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
