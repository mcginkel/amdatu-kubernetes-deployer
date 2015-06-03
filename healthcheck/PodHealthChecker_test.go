package healthcheck

import (
	"testing"
	"time"
	"net/http"
	"net/http/httptest"
	"fmt"
)

func TestWaitForPod(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"healthy": true}`)
	}))
	defer ts.Close()

	result := WaitForPodStarted(ts.URL, 100 * time.Millisecond);
	if !result {
		t.Error("Pod didn't get healthy")
	}
}

func TestTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"healthy": false}`)
	}))
	defer ts.Close()

	result := WaitForPodStarted(ts.URL, 100 * time.Millisecond);
	if result {
		t.Error("Pod reported healthy but shouldn't")
	}
}