package auth

import (
	"testing"
)

func TestAuthenticate(t *testing.T) {
	namespaces, err := AuthenticateAndGetNamespaces("http://localhost:8282", "admin@amdatu.org", "test")

	if err != nil {
		t.Fatal(err)
	}

	if len(namespaces) == 0 {
		t.Fatal("No namespaces returned")
	}

	if !StringInSet("default", namespaces) {
		t.Fatal("Default namespace expected but not returned")
	}
}

