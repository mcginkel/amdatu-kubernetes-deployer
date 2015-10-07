package cluster

import (
	"testing"
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