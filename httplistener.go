package main
import (
	"github.com/gorilla/mux"
	"net/http"
	"io/ioutil"
	"log"
	"encoding/json"
	"fmt"
	"com.amdatu.rti.deployment/bluegreen"
	"flag"
	"com.amdatu.rti.deployment/cluster"
	"errors"
	"com.amdatu.rti.deployment/rolling"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
)

var kubernetesurl, etcdUrl, port string

func init() {
	flag.StringVar(&kubernetesurl, "kubernetes", "", "URL to the Kubernetes API server")
	flag.StringVar(&etcdUrl, "etcd", "", "Url to etcd")
	flag.StringVar(&port, "deployport", "8000", "Port to listen for deployments")

	exampleUsage := "Missing required argument %v. Example usage: httplistener -kubernetes http://[kubernetes-api-url]:8080 -etcd http://[etcd-url]:2379 -deployport 8000"

	flag.Parse()

	if kubernetesurl == "" {
		log.Fatalf(exampleUsage, "kubernetes")
	}

	if etcdUrl == "" {
		log.Fatalf(exampleUsage, "etcd")
	}
}

func main() {
	r := mux.NewRouter()

	r.HandleFunc("/deployment", DeploymentHandler).Methods("POST")


	log.Printf("Listining for deployments on port %v\n", port)

	if  err := http.ListenAndServe(":" + port, r); err != nil {
		log.Fatal(err)
	}
}



func DeploymentHandler(respWriter http.ResponseWriter, req *http.Request) {
	logger := cluster.Logger{respWriter}

	defer req.Body.Close()
	body, err := ioutil.ReadAll(req.Body)

	if err != nil {
		logger.Printf("Error reading body: %v", err)
	}

	deployment := cluster.Deployment{}
	if err := json.Unmarshal(body, &deployment); err != nil {
		logger.Printf("Error parsing body: %v", err)
	}

	if len(deployment.Namespace) == 0 {
		deployment.Namespace = api.NamespaceDefault
	}

	logger.Printf("%v\n", deployment.String())

	deployer := cluster.NewDeployer(kubernetesurl, etcdUrl, deployment, &logger)
	var deploymentError error

	switch deployment.DeploymentType {
		case "blue-green":
			deploymentError = bluegreen.NewBlueGreen(deployer).Deploy();
		case "rolling":
			deploymentError = rolling.NewRollingDeployer(deployer).Deploy();
		default:
			deploymentError = errors.New(fmt.Sprintf("Unknown type of deployment: %v", deployment.DeploymentType))
	}

	if  deploymentError != nil {
		logger.Printf("Error during deployment: %v\n", deploymentError)
		logger.Println("============================ Deployment Failed =======================")
	} else {
		logger.Println("============================ Completed deployment =======================")
	}



}

