package main
import (
	"github.com/gorilla/mux"
	"net/http"
	"io/ioutil"
	"log"
	"encoding/json"
	"fmt"
	"com.amdatu.rti.deployment/bluegreen"
)

func main() {
	r := mux.NewRouter()

	r.HandleFunc("/deployment", DeploymentHandler).Methods("POST")

	if  err := http.ListenAndServe(":8000", r); err != nil {
		log.Fatal(err)
	}
}

func DeploymentHandler(respWriter http.ResponseWriter, req *http.Request) {
	defer req.Body.Close()
	body, err := ioutil.ReadAll(req.Body)

	if err != nil {
		log.Printf("Error reading body: %v", err)
	}

	deployment := bluegreen.Deployment{}
	if err := json.Unmarshal(body, &deployment); err != nil {
		log.Printf("Error parsing body: %v", err)
	}

	fmt.Printf("%v\n", deployment.String())

	if err := bluegreen.NewDeployer("http://10.100.103.7:8080", deployment).Deploy(); err != nil {
		log.Printf("Error during deployment: %v\n", err)
	} else {
		log.Println("============================ Completed deployment =======================")
	}

}
