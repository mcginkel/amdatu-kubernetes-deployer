package cluster

import (
	"testing"
	"strings"
	"com.amdatu.rti.deployment/Godeps/_workspace/src/k8s.io/kubernetes/pkg/api"
)

func TestGetHealthUrl_WithSlash(t *testing.T) {

	depl := Deployment{
		HealthCheckUrl: "/myhealth",
	}

	deployer := Deployer{
		Deployment:depl,
	}
	url := deployer.getHealthcheckUrl("127.0.0.1", 8080)

	if url != "http://127.0.0.1:8080/myhealth" {
		t.Errorf("Incorrect url: %v", url)
	}

}


func TestGetHealthUrl_WithoutSlash(t *testing.T) {

	depl := Deployment{
		HealthCheckUrl: "myhealth",
	}

	deployer := Deployer{
		Deployment:depl,
	}
	url := deployer.getHealthcheckUrl("127.0.0.1", 8080)

	if url != "http://127.0.0.1:8080/myhealth" {
		t.Errorf("Incorrect url: %v", url)
	}

}

func TestGetHealthUrl_Default(t *testing.T) {

	depl := Deployment{
		HealthCheckUrl: "",
	}

	deployer := Deployer{
		Deployment:depl,
	}
	url := deployer.getHealthcheckUrl("127.0.0.1", 8080)

	if url != "http://127.0.0.1:8080/health" {
		t.Errorf("Incorrect url: %v", url)
	}

}

func TestSetDeploymentDefaults(t *testing.T) {
	deployment := Deployment{}
	deployment.SetDefaults()

	if deployment.Namespace != "default" {
		t.Error("Defaul namespace not set")
	}

}

func TestValidateDeployment(t *testing.T) {
	deployment := Deployment{}
	err := deployment.Validate()

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
	pod := api.Pod{
		Spec:api.PodSpec{
			Containers: []api.Container{
				{Ports: []api.ContainerPort{
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
	pod := api.Pod{
		Spec:api.PodSpec{
			Containers: []api.Container{
				{Ports: []api.ContainerPort{
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
	pod := api.Pod{
		Spec:api.PodSpec{
			Containers: []api.Container{{}},
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
	pod := api.Pod{
		Spec:api.PodSpec{
			Containers: []api.Container{
				{Ports: []api.ContainerPort{
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