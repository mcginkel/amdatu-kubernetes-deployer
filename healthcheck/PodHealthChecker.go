package healthcheck
import (
	"log"
	"fmt"
	"net/http"
	"io/ioutil"
	"encoding/json"
	"time"
)


func WaitForPodStarted(host string, port int, timeoutDuration time.Duration) bool {

	timeout := make(chan string)
	callBack := make(chan bool)

	go func() {
		time.Sleep(timeoutDuration)
		timeout <- "TIMEOUT"
	}()


	go watchPod(host,port, callBack)

	select {
	case <- callBack:
		return true
	case <- timeout:
		callBack <- false
		log.Println("Timeout waiting for Pod to become healthy")
		return false
	}
}

func watchPod(host string, port int, callback chan bool) {
	var resp *http.Response
	var err error

	for {
		select {
		case <-callback:
			return
		default:
			resp, err = http.Post(fmt.Sprintf("http://%v:%v/health", host, port), "application/json", nil)
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
			fmt.Println(dat.Healthy)

			if dat.Healthy {
				callback <- true
			}

			log.Println("Not yet healthy, retrying...")
			time.Sleep(time.Second * 2)
		}
	}
}

type HealthCheckEvent struct {
	Healthy bool `json:"healthy,omitempty"`
}