package etcdregistry

import "testing"

func TestEnvVarRename(t *testing.T) {
	result := fixEnvVarName("my-example-key")
	if result != "MY_EXAMPLE_KEY" {
		t.Error("Invalid rename of environement variable")
	}
}
