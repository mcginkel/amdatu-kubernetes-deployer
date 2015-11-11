package cluster

import (
	"testing"
	"strings"
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