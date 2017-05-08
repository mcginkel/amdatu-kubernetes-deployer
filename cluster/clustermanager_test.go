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
	"strings"
	"testing"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	"k8s.io/client-go/pkg/api/v1"
)

func TestGetHealthUrl_WithSlash(t *testing.T) {

	deployer := Deployer{
		Deployment: &types.Deployment{
			Descriptor: &types.Descriptor{
				HealthCheckPath: "/myhealth",
			},
		},
	}
	url := deployer.GetHealthcheckUrl("127.0.0.1", 8080)

	if url != "http://127.0.0.1:8080/myhealth" {
		t.Errorf("Incorrect url: %v", url)
	}

}

func TestGetHealthUrl_WithoutSlash(t *testing.T) {

	deployer := Deployer{
		Deployment: &types.Deployment{
			Descriptor: &types.Descriptor{
				HealthCheckPath: "myhealth",
			},
		},
	}
	url := deployer.GetHealthcheckUrl("127.0.0.1", 8080)

	if url != "http://127.0.0.1:8080/myhealth" {
		t.Errorf("Incorrect url: %v", url)
	}

}

func TestGetHealthUrl_Default(t *testing.T) {

	deployer := Deployer{
		Deployment: &types.Deployment{
			Descriptor: &types.Descriptor{
				HealthCheckPath: "",
			},
		},
	}
	url := deployer.GetHealthcheckUrl("127.0.0.1", 8080)

	if url != "http://127.0.0.1:8080/health" {
		t.Errorf("Incorrect url: %v", url)
	}

}

func TestSetDeploymentDefaults(t *testing.T) {
	descriptor := types.Descriptor{}
	descriptor.SetDefaults()

	if descriptor.Namespace != "default" {
		t.Error("Defaul namespace not set")
	}
}

func TestValidateDeployment(t *testing.T) {
	descriptor := types.Descriptor{}
	err := descriptor.Validate()

	if err == nil {
		t.Error("Validate should fail on an empty Deployment")
	} else {
		if !strings.Contains(err.Error(), "Missing required property 'appName'") {
			t.Error("Error message not correct")
		}
	}
}

func TestDetermineNextVersionIncorrect(t *testing.T) {
	newVersion, err := DetermineNewVersion("1.1a")
	if err == nil {
		t.Error("Expected error for invalid incremental version " + newVersion)
	}
}

func TestDetermineNextVersionCorrect(t *testing.T) {
	newVersion, err := DetermineNewVersion("1")
	if err != nil {
		t.Error("Unexpected error for valid incremental version")
	} else if newVersion != "2" {
		t.Error("Unexpected new version " + newVersion)
	}

}

/**
Default to only exposed port
*/
func TestFindHealthcheckPort_SinglePort(t *testing.T) {
	pod := v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Ports: []v1.ContainerPort{
					{ContainerPort: 8080},
				}},
			},
		},
	}

	port := FindHealthcheckPort(&pod)
	if port != 8080 {
		t.Error("Invalid port found for pod")
	}
}

/**
Named port when multiple ports defined
*/
func TestFindHealthcheckPort_MultiplePort(t *testing.T) {
	pod := v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Ports: []v1.ContainerPort{
					{ContainerPort: 8080, Name: "web"},
					{ContainerPort: 9999, Name: "healthcheck"},
				}},
			},
		},
	}

	port := FindHealthcheckPort(&pod)
	if port != 9999 {
		t.Error("Invalid port found for pod")
	}
}

/**
Default port when no ports are defined
*/
func TestFindHealthcheckPort_NoPort(t *testing.T) {
	pod := v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{{}},
		},
	}

	port := FindHealthcheckPort(&pod)
	if port != 9999 {
		t.Error("Invalid port found for pod")
	}
}

/**
Default port when no health check port found
*/
func TestFindHealthcheckPort_NoHealthPort(t *testing.T) {
	pod := v1.Pod{
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{Ports: []v1.ContainerPort{
					{ContainerPort: 8080, Name: "web"},
					{ContainerPort: 9999, Name: "db"},
				}},
			},
		},
	}

	port := FindHealthcheckPort(&pod)
	if port != 8080 {
		t.Error("Invalid port found for pod")
	}
}
