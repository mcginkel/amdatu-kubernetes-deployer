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

	"bitbucket.org/amdatulabs/amdatu-kubernetes-go/api/v1"
)

const DNS952LabelFmt string = "[a-z]([-a-z0-9]*[a-z0-9])?"

var dns952LabelRegexp = regexp.MustCompile("^" + DNS952LabelFmt + "$")

type Deployment struct {
	Id                      string            `json:"id,omitempty"`
	WebHooks                []WebHook         `json:"webhooks,omitempty"`
	DeploymentType          string            `json:"deploymentType,omitempty"`
	NewVersion              string            `json:"newVersion,omitempty"`
	DeployedVersion         string            `json:"deployedVersion,omitempty"`
	AppName                 string            `json:"appName,omitempty"`
	Replicas                int               `json:"replicas,omitempty"`
	Frontend                string            `json:"frontend,omitempty"`
	RedirectWww             bool              `json:"redirectWww,omitempty"`
	PodSpec                 v1.PodSpec        `json:"podspec,omitempty"`
	Namespace               string            `json:"namespace,omitempty"`
	Email                   string            `json:"email,omitempty"`
	Password                string            `json:"password,omitempty"`
	Environment             map[string]string `json:"environment,omitempty"`
	UseCompression          bool              `json:"useCompression,omitempty"`
	AdditionHttpHeaders     []HttpHeader      `json:"additionHttpHeaders,omitempty"`
	UseHealthCheck          bool              `json:"useHealthCheck,omitempty"`
	HealthCheckPath         string            `json:"healthCheckPath,omitempty"`
	HealthCheckPort         int               `json:"healthCheckPort,omitempty"`
	HealthCheckType         string            `json:"healthCheckType,omitempty"`
	IgnoreHealthCheck       bool              `json:"ignoreHealthCheck,omitempty"`
	UseExternalHealthCheck  bool              `json:"useExternalHealthCheck,omitempty"`
	ExternalHealthCheckPath string            `json:"externalHealthCheckPath,omitempty"`
	DeploymentTs            string            `json:"deploymentTs,omitempty"`
}

type DeploymentResult struct {
	Date            string     `json:"date,omitempty"`
	Status          string     `json:"status,omitempty"`
	Deployment      Deployment `json:"deployment,omitempty"`
	HealthcheckData string     `json:"healthcheckData,omitempty"`
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

type HttpHeader struct {
	Header string `json:"Header,omitempty"`
	Value  string `json:"Value,omitempty"`
}

type User struct {
	Email    string `json:"email,omitempty"`
	Password string `json:"password,omitempty"`
}

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
	deployment.AppName = strings.ToLower(deployment.AppName)

	if deployment.NewVersion == "#" {
		//Make sure to pass validation, but assume a version of 3 characters. Value will be replaced later
		deployment.DeployedVersion = "000"
	} else {
		deployment.NewVersion = strings.Replace(deployment.NewVersion, ".", "-", -1)
		deployment.NewVersion = strings.Replace(deployment.NewVersion, "_", "-", -1)
		deployment.NewVersion = strings.ToLower(deployment.NewVersion)
		deployment.DeployedVersion = deployment.NewVersion
	}

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

	if deployment.DeployedVersion == "" {
		messageBuffer.WriteString("Missing required property 'deployedVersion'\n")
	}

	if len(deployment.PodSpec.Containers) == 0 {
		messageBuffer.WriteString("No containers specified in PodSpec\n")
	}

	for i, container := range deployment.PodSpec.Containers {
		if container.Image == "" {
			messageBuffer.WriteString(fmt.Sprintf("No image specified for container %v\n", i))
		}
	}

	appName := deployment.AppName + "-" + deployment.DeployedVersion
	if len(appName) > 24 {
		messageBuffer.WriteString(fmt.Sprintf("Application name %v is too long. A maximum of 24 characters is allowed\n", appName))
	}

	if !dns952LabelRegexp.MatchString(appName) {
		messageBuffer.WriteString(fmt.Sprintf("Application name %v doesn't match pattern [a-z]([-a-z0-9]*[a-z0-9])?\n", appName))
	}

	if strings.Contains(deployment.Frontend, "://") {
		messageBuffer.WriteString(fmt.Sprintf("Frontend Url %v must not contain the protocol (e.g. https://)\n", deployment.Frontend))
	}

	message := messageBuffer.String()

	if len(message) > 0 {
		return errors.New(message)
	}

	return nil
}
