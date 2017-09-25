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
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/etcdregistry"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/helper"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/pkg/api/v1"
)

const DNS952LabelFmt string = "[a-z]([-a-z0-9]*[a-z0-9])?"

var dns952LabelRegexp = regexp.MustCompile("^" + DNS952LabelFmt + "$")

type ClusterManager struct {
	Config     *helper.DeployerConfig
	Deployment *types.Deployment
	Registry   *etcdregistry.EtcdRegistry
	Logger     logger.Logger
}

func NewClusterManager(config helper.DeployerConfig, deployment *types.Deployment, registry *etcdregistry.EtcdRegistry, logger logger.Logger) *ClusterManager {

	return &ClusterManager{&config, deployment, registry, logger}

}

func (cm *ClusterManager) CreateReplicationController() (*v1.ReplicationController, error) {

	descriptor := cm.Deployment.Descriptor

	rcName := cm.Deployment.GetVersionedName()
	ctrl := new(v1.ReplicationController)
	ctrl.Name = rcName

	labels := make(map[string]string)
	labels["name"] = rcName
	labels["version"] = cm.Deployment.Version
	labels["app"] = descriptor.AppName

	ctrl.Labels = labels

	annotations := make(map[string]string)
	annotations["deploymentTs"] = cm.Deployment.Created
	annotations["deploymentId"] = cm.Deployment.Id
	annotations["appName"] = descriptor.AppName
	annotations["version"] = cm.Deployment.Version
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
			v1.EnvVar{Name: "APP_VERSION", Value: cm.Deployment.Version},
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
			"version": cm.Deployment.Version,
			"app":     descriptor.AppName,
		},
		Replicas: &replicas,
		Template: &v1.PodTemplateSpec{
			ObjectMeta: meta.ObjectMeta{
				Labels: map[string]string{
					"name":    rcName,
					"version": cm.Deployment.Version,
					"app":     descriptor.AppName,
				},
			},
			Spec: descriptor.PodSpec,
		},
	}

	result, err := cm.Config.K8sClient.CreateReplicationController(descriptor.Namespace, ctrl)
	if err != nil {
		cm.Logger.Println("Error while creating replication controller")
		return result, err
	}

	cm.Logger.Printf("Replication Controller %v created\n", result.ObjectMeta.Name)
	return result, nil

}

func (cm *ClusterManager) CreateService() (*v1.Service, error) {

	descriptor := cm.Deployment.Descriptor

	name := cm.Deployment.GetVersionedName()
	srv := new(v1.Service)
	srv.Name = name

	selector := make(map[string]string)
	selector["name"] = name
	selector["version"] = cm.Deployment.Version
	selector["app"] = descriptor.AppName

	srv.Labels = selector

	ports := []v1.ServicePort{}
	for _, container := range descriptor.PodSpec.Containers {
		for _, port := range container.Ports {
			servicePort := v1.ServicePort{
				Name:       port.Name,
				Port:       port.ContainerPort,
				TargetPort: intstr.IntOrString{Type: intstr.Int, IntVal: port.ContainerPort},
				Protocol:   v1.ProtocolTCP,
			}
			ports = append(ports, servicePort)
		}
	}

	srv.Spec = v1.ServiceSpec{
		Selector:        selector,
		Ports:           ports,
		Type:            v1.ServiceTypeClusterIP,
		SessionAffinity: "None",
	}

	cm.Logger.Println("Creating Service")

	return cm.Config.K8sClient.CreateService(descriptor.Namespace, srv)
}

func (cm *ClusterManager) CreateOrUpdatePersistentService() (*v1.Service, error) {

	deployment := cm.Deployment
	descriptor := deployment.Descriptor

	svc, err := cm.Config.K8sClient.GetService(descriptor.Namespace, descriptor.AppName)
	if err == nil {
		cm.Logger.Printf("Persistent service %v already exists on IP %v", svc.Name, svc.Spec.ClusterIP)

		// update port, they might have changed
		cm.Logger.Println("Updating persistent service ports")
		newPorts := getPorts(descriptor.PodSpec.Containers)
		svc.Spec.Ports = newPorts

		cm.Logger.Println("Updating persistent service version selector")
		deployment.OldVersion = svc.Spec.Selector["version"]
		svc.Spec.Selector["version"] = deployment.Version

		// update session affinity, will be handled by nginx
		svc.Spec.SessionAffinity = "None"

		// update service type, used to be NodePort, which is not needed
		svc.Spec.Type = v1.ServiceTypeClusterIP

		_, err := cm.Config.K8sClient.UpdateService(descriptor.Namespace, svc)
		if err != nil {
			cm.Logger.Println("Error updating persistent service: " + err.Error())
			return svc, err
		}

		return svc, nil

	} else if statusError, isStatus := err.(*errors.StatusError); isStatus && statusError.Status().Reason == meta.StatusReasonNotFound {
		svc := new(v1.Service)
		svc.Name = descriptor.AppName

		labels := make(map[string]string)
		labels["app"] = descriptor.AppName
		labels["name"] = descriptor.AppName
		labels["persistent"] = "true"

		svc.Labels = labels

		ports := getPorts(descriptor.PodSpec.Containers)

		selector := make(map[string]string)
		selector["app"] = descriptor.AppName
		selector["version"] = deployment.Version

		svc.Spec = v1.ServiceSpec{
			Selector:        selector,
			Ports:           ports,
			Type:            v1.ServiceTypeClusterIP,
			SessionAffinity: "None",
		}

		created, err := cm.Config.K8sClient.CreateService(descriptor.Namespace, svc)

		if err == nil {
			cm.Logger.Printf("Creating persistent service %v. Listening on IP %v\n", created.Name, created.Spec.ClusterIP)
		}
		return created, nil
	} else {
		return nil, err
	}
}

func (cm *ClusterManager) DeleteOrResetPersistentService() {

	deployment := cm.Deployment
	descriptor := deployment.Descriptor

	svc, err := cm.Config.K8sClient.GetService(descriptor.Namespace, descriptor.AppName)
	if err != nil {
		cm.Logger.Printf("Error getting persistent service: %v!", err.Error())
		return
	}

	if len(deployment.OldVersion) > 0 {
		cm.Logger.Printf("Resetting persistent service to version %v", deployment.OldVersion)
		svc.Spec.Selector["version"] = deployment.OldVersion
		_, err := cm.Config.K8sClient.UpdateService(descriptor.Namespace, svc)
		if err != nil {
			cm.Logger.Println("Error updating persistent service: " + err.Error())
		}
	} else {
		cm.Logger.Println("Deleting persistent service")
		err := cm.Config.K8sClient.DeleteService(descriptor.Namespace, svc.Name)
		if err != nil {
			cm.Logger.Println("Error deleting persistent service: " + err.Error())
		}
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

func (cm *ClusterManager) FindOldReplicationControllers() ([]v1.ReplicationController, error) {

	descriptor := cm.Deployment.Descriptor

	result := []v1.ReplicationController{}

	selector := map[string]string{"app": descriptor.AppName}
	replicationControllers, err := cm.Config.K8sClient.ListReplicationControllersWithSelector(descriptor.Namespace, selector)
	if err != nil {
		return nil, err
	}

	for _, rc := range replicationControllers.Items {
		if rc.Labels["version"] != cm.Deployment.Version {
			result = append(result, rc)
		}
	}

	return result, nil
}

func (cm *ClusterManager) FindOldPods() ([]v1.Pod, error) {

	descriptor := cm.Deployment.Descriptor

	result := []v1.Pod{}

	selector := map[string]string{"app": descriptor.AppName}
	pods, err := cm.Config.K8sClient.ListPodsWithSelector(descriptor.Namespace, selector)
	if err != nil {
		return result, err
	}

	for _, pod := range pods.Items {
		if pod.Labels["version"] != cm.Deployment.Version {
			result = append(result, pod)
		}
	}

	return result, nil
}

func (cm *ClusterManager) FindOldServices() ([]v1.Service, error) {

	descriptor := cm.Deployment.Descriptor

	result := []v1.Service{}

	selector := map[string]string{"app": descriptor.AppName}
	services, err := cm.Config.K8sClient.ListServicesWithSelector(descriptor.Namespace, selector)
	if err != nil {
		return result, err
	}

	for _, service := range services.Items {
		if service.Labels["version"] != cm.Deployment.Version && service.Labels["persistent"] != "true" {
			result = append(result, service)
		}
	}

	return result, nil
}

func (cm *ClusterManager) CleanUpOldDeployments() {
	cm.Logger.Println("Looking for old ReplicationControllers...")
	controllers, err := cm.FindOldReplicationControllers()
	if err == nil {
		for _, rc := range controllers {
			if rc.Name != "" {
				if err := cm.Config.K8sClient.ShutdownReplicationController(&rc, cm.Logger); err != nil {
					cm.Logger.Printf("Error during shutting down Replication Controller: %v", err.Error())
				}
			}
		}
	}

	cm.Logger.Println("Looking for old Pods...")
	pods, err := cm.FindOldPods()
	if err == nil {
		for _, pod := range pods {
			if pod.Name != "" {
				cm.DeletePod(pod)
			}
		}
	}

	cm.Logger.Println("Looking for old Services...")
	services, err := cm.FindOldServices()
	if err == nil {
		for _, service := range services {
			if service.Name != "" {
				cm.deleteService(service)
			}
		}
	}
}

func (cm *ClusterManager) DeletePod(pod v1.Pod) {

	descriptor := cm.Deployment.Descriptor

	cm.Logger.Printf("Deleting Pod %v", pod.Name)

	cm.Config.K8sClient.DeletePod(descriptor.Namespace, pod.Name)
}

func (cm *ClusterManager) deleteService(service v1.Service) {
	descriptor := cm.Deployment.Descriptor
	cm.Logger.Printf("Deleting Service %v", service.Name)
	cm.Config.K8sClient.DeleteService(descriptor.Namespace, service.Name)
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

func (cm *ClusterManager) GetHealthcheckUrl(host string, port int32) string {

	descriptor := cm.Deployment.Descriptor

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

func (cm *ClusterManager) findRcForDeployment() (*v1.ReplicationController, error) {
	descriptor := cm.Deployment.Descriptor
	return cm.Config.K8sClient.GetReplicationController(descriptor.Namespace, cm.Deployment.GetVersionedName())
}

func (cm *ClusterManager) findServiceForDeployment() (*v1.Service, error) {
	descriptor := cm.Deployment.Descriptor
	return cm.Config.K8sClient.GetService(descriptor.Namespace, cm.Deployment.GetVersionedName())
}

func (cm *ClusterManager) findPodsForDeployment() (*v1.PodList, error) {
	descriptor := cm.Deployment.Descriptor
	selector := map[string]string{"app": descriptor.AppName, "version": cm.Deployment.Version}
	return cm.Config.K8sClient.ListPodsWithSelector(descriptor.Namespace, selector)
}

func (cm *ClusterManager) CleanupFailedDeployment() {
	cm.Logger.Println("Cleaning up resources created by deployment")

	cm.DeleteOrResetPersistentService()

	rc, err := cm.findRcForDeployment()
	if err == nil {
		cm.Logger.Printf("  Deleting ReplicationController %v", rc.Name)
		cm.Config.K8sClient.ShutdownReplicationController(rc, cm.Logger)
	}

	pods, err := cm.findPodsForDeployment()
	if err == nil {
		for _, pod := range pods.Items {
			cm.Logger.Printf("  Deleting Pod %v", pod.Name)
			cm.DeletePod(pod)
		}
	}

	service, err := cm.findServiceForDeployment()
	if err == nil {
		cm.Logger.Printf("  Deleting Service %v", service.Name)
		cm.deleteService(*service)
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
