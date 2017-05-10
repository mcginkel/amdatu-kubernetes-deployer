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
package k8s

import (
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"
	"testing"
)

func TestNamespaces(t *testing.T) {
	k8sConfig := K8sConfig{
		ApiServerUrl: "http://10.150.16.32:8080",
	}
	client := New(k8sConfig, logger.NewConsoleLogger())

	name := "sometest"
	client.DeleteNamespace(name)

	ns, err := client.GetNamespace(name)
	if err == nil {
		t.Error("expected not found error", err)
	} else if statusError, isStatus := err.(*errors.StatusError); isStatus && statusError.Status().Reason == meta.StatusReasonNotFound {
		t.Log("namespace not found, correct")
	} else {
		t.Error("unknown error", ns)
	}

	newNs := v1.Namespace{
		meta.TypeMeta{},
		meta.ObjectMeta{Name: name},
		v1.NamespaceSpec{},
		v1.NamespaceStatus{},
	}
	ns, err = client.CreateNamespace(&newNs)
	if err != nil {
		t.Error("error creating namespace", err)
	}
	if ns.Name != name {
		t.Error("namespace has wrong name")
	}

	ns, err = client.GetNamespace(name)
	if err != nil {
		t.Error("error getting namespace", err)
	}
	if ns.Name != name {
		t.Error("namespace has wrong name")
	}

	err = client.DeleteNamespace(name)
	if err != nil {
		t.Error("error deleting namespace")
	}

}
