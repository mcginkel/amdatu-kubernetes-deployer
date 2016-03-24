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
package auth

import (
	"fmt"
	"github.com/gorilla/mux"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAuthenticate(t *testing.T) {

	ts := httptest.NewServer(Handlers())
	defer ts.Close()

	namespaces, err := AuthenticateAndGetNamespaces(ts.URL, "admin@amdatu.org", "test")

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

func Handlers() *mux.Router {
	r := mux.NewRouter()

	r.HandleFunc("/auth/login", loginHandler).Methods("POST")

	r.HandleFunc("/rtiauth/namespaces", namespacesHandler).Methods("GET")

	return r
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, `{"roles": ["DEPLOYER"]}`)
}

func namespacesHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintln(w, `["default", "test"]`)
}
