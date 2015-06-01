package healthcheck

import (
	"testing"
	"time"
)

func TestWaitForPod(t *testing.T) {
	result := WaitForPodStarted("localhost", 8080, 2 * time.Second);
	if !result {
		t.Error("Pod didn't get healthy")
	}
}