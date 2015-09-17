package auth

import (
	"testing"
)

func TestAuthenticate(t *testing.T) {
	namespaces, err := AuthenticateAndGetNamespaces("http://localhost:8282", "admin@amdatu.org", "test")

	if err != nil {
		t.Error(err)
	}

	if len(namespaces) == 0 {
		t.Error("No namespaces returned")
	}

	if !NameSpaceInSet("default", namespaces) {
		t.Error("Default namespace expected but not returned")
	}
}

