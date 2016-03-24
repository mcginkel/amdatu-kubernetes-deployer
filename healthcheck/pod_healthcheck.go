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
package healthcheck

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

func WaitForPodStarted(url string, timeoutDuration time.Duration) bool {
	timeout := make(chan string)
	callBack := make(chan bool)

	go func() {
		time.Sleep(timeoutDuration)
		timeout <- "TIMEOUT"
		close(timeout)
	}()

	go watchPod(url, callBack)

	select {
	case <-callBack:
		log.Println("Pod turned healthy")
		return true
	case <-timeout:
		callBack <- false
		log.Println("Timeout waiting for Pod to become healthy")
		return false
	}
}

func watchPod(url string, callback chan bool) {
	var resp *http.Response
	var err error

	log.Printf("Health checking on %v\n", url)

	for {
		select {
		case <-callback:
			return
		default:
			resp, err = http.Post(url, "application/json", nil)
			if err != nil {
				log.Println("Error connecting, retrying...")
				time.Sleep(time.Second * 2)

				continue
			}
			defer resp.Body.Close()
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				log.Println("Error reading body, retrying...")
				time.Sleep(time.Second * 2)
				continue
			}

			var dat = HealthCheckEvent{}
			if err := json.Unmarshal(body, &dat); err != nil {
				log.Println("Invalid json, retrying...")
				time.Sleep(time.Second * 2)
				continue
			}

			if dat.Healthy {
				callback <- true
				close(callback)
				return
			}

			log.Println("Not yet healthy, retrying...")
			time.Sleep(time.Second * 2)
		}
	}
}

type HealthCheckEvent struct {
	Healthy bool `json:"healthy,omitempty"`
}
