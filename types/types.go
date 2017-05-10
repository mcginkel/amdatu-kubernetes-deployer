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
package types

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"k8s.io/client-go/pkg/api/v1"
)

const DEPLOYMENTSTATUS_DEPLOYING = "DEPLOYING"
const DEPLOYMENTSTATUS_DEPLOYED = "DEPLOYED"
const DEPLOYMENTSTATUS_UNDEPLOYING = "UNDEPLOYING"
const DEPLOYMENTSTATUS_UNDEPLOYED = "UNDEPLOYED"
const DEPLOYMENTSTATUS_FAILURE = "FAILURE"

const DNS952LabelFmt string = "[a-z]([-a-z0-9]*[a-z0-9])?"

var dns952LabelRegexp = regexp.MustCompile("^" + DNS952LabelFmt + "$")

type Descriptor struct {
	Id                         string            `json:"id,omitempty"`
	Created                    string            `json:"created,omitempty"`
	LastModified               string            `json:"lastModified,omitempty"`
	WebHooks                   []WebHook         `json:"webhooks,omitempty"`
	DeploymentType             string            `json:"deploymentType,omitempty"`
	NewVersion                 string            `json:"newVersion,omitempty"`
	AppName                    string            `json:"appName,omitempty"`
	Replicas                   int               `json:"replicas,omitempty"`
	Frontend                   string            `json:"frontend,omitempty"`
	RedirectWww                bool              `json:"redirectWww,omitempty"`
	PodSpec                    v1.PodSpec        `json:"podspec,omitempty"`
	Namespace                  string            `json:"namespace,omitempty"`
	Email                      string            `json:"email,omitempty"`
	Password                   string            `json:"password,omitempty"`
	Environment                map[string]string `json:"environment,omitempty"`
	UseCompression             bool              `json:"useCompression,omitempty"`
	UseStickySessions          bool              `json:"useStickySessions,omitempty"`
	TlsSecretName              string            `json:"tlsSecretName,omitempty"`
	AdditionHttpHeaders        []HttpHeader      `json:"additionHttpHeaders,omitempty"`
	UseHealthCheck             bool              `json:"useHealthCheck,omitempty"`
	HealthCheckPath            string            `json:"healthCheckPath,omitempty"`
	HealthCheckPort            int               `json:"healthCheckPort,omitempty"`
	HealthCheckType            string            `json:"healthCheckType,omitempty"`
	IgnoreHealthCheck          bool              `json:"ignoreHealthCheck,omitempty"`
	UseExternalHealthCheck     bool              `json:"useExternalHealthCheck,omitempty"`
	ExternalHealthCheckPath    string            `json:"externalHealthCheckPath,omitempty"`
	Deprecated_DeployedVersion string            `json:"deployedVersion,omitempty"`
	Deprecated_DeploymentTs    string            `json:"deploymentTs,omitempty"`
}

func (descriptor *Descriptor) SetDefaults() *Descriptor {

	if len(descriptor.Namespace) == 0 {
		descriptor.Namespace = v1.NamespaceDefault
	}

	if len(descriptor.DeploymentType) == 0 {
		descriptor.DeploymentType = "blue-green"
	}

	if descriptor.Replicas <= 0 {
		descriptor.Replicas = 1
	}

	if len(descriptor.PodSpec.RestartPolicy) == 0 {
		descriptor.PodSpec.RestartPolicy = v1.RestartPolicyAlways
	}
	if len(descriptor.PodSpec.DNSPolicy) == 0 {
		descriptor.PodSpec.DNSPolicy = v1.DNSClusterFirst
	}

	for i := range descriptor.PodSpec.Containers {
		container := descriptor.PodSpec.Containers[i]
		if len(container.ImagePullPolicy) == 0 {
			container.ImagePullPolicy = v1.PullAlways
		}

		for j := range container.Ports {
			if len(container.Ports[j].Protocol) == 0 {
				container.Ports[j].Protocol = v1.ProtocolTCP
			}
		}
		descriptor.PodSpec.Containers[i] = container
	}

	descriptor.AppName = strings.Replace(descriptor.AppName, ".", "-", -1)
	descriptor.AppName = strings.Replace(descriptor.AppName, "_", "-", -1)
	descriptor.AppName = strings.ToLower(descriptor.AppName)

	return descriptor
}

func (descriptor *Descriptor) Validate() error {

	var messageBuffer bytes.Buffer

	//Currently only blue-green deployments are supported
	if descriptor.DeploymentType != "blue-green" {
		messageBuffer.WriteString(fmt.Sprintf("Unsupported deploymentType '%v'\n", descriptor.DeploymentType))
	}

	if descriptor.AppName == "" {
		messageBuffer.WriteString("Missing required property 'appName'\n")
	}

	if descriptor.Namespace == "" {
		messageBuffer.WriteString("Missing required property 'namespace'\n")
	}

	if descriptor.NewVersion == "" {
		messageBuffer.WriteString("Missing required property 'newVersion'\n")
	}

	if len(descriptor.PodSpec.Containers) == 0 {
		messageBuffer.WriteString("No containers specified in PodSpec\n")
	}

	for i, container := range descriptor.PodSpec.Containers {
		if container.Image == "" {
			messageBuffer.WriteString(fmt.Sprintf("No image specified for container %v\n", i))
		}
	}

	version := descriptor.NewVersion
	if version == "#" {
		version = "000"
	}
	appName := descriptor.AppName + "-" + version
	if len(appName) > 24 {
		messageBuffer.WriteString(fmt.Sprintf("Application name %v is too long. A maximum of 24 characters is allowed\n", appName))
	}

	if !dns952LabelRegexp.MatchString(appName) {
		messageBuffer.WriteString(fmt.Sprintf("Application name %v doesn't match pattern [a-z]([-a-z0-9]*[a-z0-9])?\n", appName))
	}

	if strings.Contains(descriptor.Frontend, "://") {
		messageBuffer.WriteString(fmt.Sprintf("Frontend Url %v must not contain the protocol (e.g. https://)\n", descriptor.Frontend))
	}

	message := messageBuffer.String()

	if len(message) > 0 {
		return errors.New(message)
	}

	return nil
}

func (descriptor *Descriptor) String() string {
	b, err := json.MarshalIndent(descriptor, "", "    ")

	if err != nil {
		return "Error writing deployment to JSON"
	}

	return string(b)
}

type Deployment struct {
	Id           string      `json:"id,omitempty"`
	Created      string      `json:"created,omitempty"`
	LastModified string      `json:"lastModified,omitempty"`
	Version      string      `json:"version,omitempty"`
	Status       string      `json:"status,omitempty"`
	Descriptor   *Descriptor `json:"descriptor,omitempty"`
}

func (deployment *Deployment) SetVersion() {
	version := deployment.Descriptor.NewVersion
	if version == "#" {
		//Make sure to pass validation, but assume a version of 3 characters. Value will be replaced later
		deployment.Version = "000"
	} else {
		version = strings.Replace(version, ".", "-", -1)
		version = strings.Replace(version, "_", "-", -1)
		version = strings.ToLower(version)
		deployment.Version = version
	}
}

func (deployment *Deployment) GetVersionedName() string {
	return deployment.Descriptor.AppName + "-" + deployment.Version
}

func (deployment *Deployment) String() string {
	b, err := json.MarshalIndent(deployment, "", "    ")

	if err != nil {
		return "Error writing deployment to JSON"
	}

	return string(b)
}

type WebHook struct {
	Description string `json:"description,omitempty"`
	Key         string `json:"key,omitempty"`
}

type HttpHeader struct {
	Header string `json:"Header,omitempty"`
	Value  string `json:"Value,omitempty"`
}

type HealthData struct {
	PodName string `json:"podName"`
	Value   string `json:"value"`
}
