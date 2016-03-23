package auth

import (
	"testing"
	"net/http/httptest"
	"net/http"
	"fmt"
	"github.com/gorilla/mux"
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

func Handlers() *mux.Router{
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