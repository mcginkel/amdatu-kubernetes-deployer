package main

import (
	"com.amdatu.rti.deployment/Godeps/_workspace/src/github.com/gorilla/mux"
	"com.amdatu.rti.deployment/auth"
	"com.amdatu.rti.deployment/bluegreen"
	"com.amdatu.rti.deployment/cluster"
	"com.amdatu.rti.deployment/redeploy"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"com.amdatu.rti.deployment/deploymentregistry"
)

var kubernetesurl, etcdUrl, port, dashboardurl, kubernetesUsername, kubernetesPassword string
var mutex = &sync.Mutex{}

func init() {
	flag.StringVar(&kubernetesurl, "kubernetes", "", "URL to the Kubernetes API server")
	flag.StringVar(&etcdUrl, "etcd", "", "Url to etcd")
	flag.StringVar(&port, "deployport", "8000", "Port to listen for deployments")
	flag.StringVar(&dashboardurl, "dashboardurl", "noauth", "Dashboard url to use for authentication. Skip authentication when not set.")
	flag.StringVar(&kubernetesUsername, "kubernetesusername", "noauth", "Username to authenticate against Kubernetes API server. Skip authentication when not set")
	flag.StringVar(&kubernetesPassword, "kubernetespassword", "noauth", "Username to authenticate against Kubernetes API server.")

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

	fmt.Printf("Dployer started and listening on port %v\n", port)

	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal(err)
	}
}

type DeploymentRequest struct {
	ResponseWriter http.ResponseWriter
	Req            *http.Request
}

func DeploymentHandler(responseWriter http.ResponseWriter, req *http.Request) {
	mutex.Lock()

	logger := cluster.NewLogger(responseWriter)

	defer req.Body.Close()
	body, err := ioutil.ReadAll(req.Body)

	if err != nil {
		logger.Printf("Error reading body: %v", err)
	}

	deployment := cluster.Deployment{}
	if err := json.Unmarshal(body, &deployment); err != nil {
		logger.Printf("Error parsing body: %v", err)
	}

	if err = deployment.SetDefaults().Validate(); err != nil {
		logger.Printf("Deployment descriptor incorrect: \n %v", err.Error())
		returnError(err, responseWriter, logger)

		logger.Flush()
		mutex.Unlock()

		return
	}

	logger.Printf("%v\n", deployment.String())

	if dashboardurl != "noauth" {
		namespaces, err := auth.AuthenticateAndGetNamespaces(dashboardurl, deployment.Email, deployment.Password)

		if err != nil {
			logger.Println("Could not authenticate: ", err)
			returnError(err, responseWriter, logger)

			logger.Flush()
			mutex.Unlock()
			return
		}

		if !auth.StringInSet(deployment.Namespace, namespaces) {
			logger.Printf("User %v not authorised to namespace %v", "admin@amdatu.org", deployment.Namespace)
			returnError(err, responseWriter, logger)

			logger.Flush()
			mutex.Unlock()
			return
		}
	}

	deployer := cluster.NewDeployer(kubernetesurl, kubernetesUsername, kubernetesPassword, etcdUrl, deployment, &logger)
	deploymentregistry.NewDeploymentRegistry(deployer.EtcdClient)


	var deploymentError error

	/*Check if namespace has the current version deployed
	If so, switch to redeployer
	*/

	logger.Println("Checking for existing service...")
	_, err = deployer.K8client.Services(deployment.Namespace).Get(deployer.CreateRcName())

	if err != nil {
		logger.Println("No existing service found, starting deployment")

		switch deployment.DeploymentType {
		case "blue-green":
			deploymentError = bluegreen.NewBlueGreen(deployer).Deploy()
		default:
			deploymentError = errors.New(fmt.Sprintf("Unknown type of deployment: %v", deployment.DeploymentType))
		}
	} else {
		logger.Println("Existing service found. Using redeployer")
		deploymentError = redeploy.NewRedeployer(deployer).Deploy()
	}

	if deploymentError != nil {
		returnError(deploymentError, responseWriter, logger)
		deployer.CleanupFailedDeployment()
	} else {
		logger.Println("============================ Completed deployment =======================")
	}

	logger.Flush()
	mutex.Unlock()

}

func returnError(deploymentError error, responseWriter http.ResponseWriter, logger cluster.Logger) {
	responseWriter.WriteHeader(500)
	logger.Printf("Error during deployment: %v\n", deploymentError)
	logger.Println("============================ Deployment Failed =======================")
}