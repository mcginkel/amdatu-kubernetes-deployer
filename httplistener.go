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
	"com.amdatu.rti.deployment/redeploy"
	"com.amdatu.rti.deployment/auth"
)

var kubernetesurl, etcdUrl, port, dashboardurl string

func init() {
	flag.StringVar(&kubernetesurl, "kubernetes", "", "URL to the Kubernetes API server")
	flag.StringVar(&etcdUrl, "etcd", "", "Url to etcd")
	flag.StringVar(&port, "deployport", "8000", "Port to listen for deployments")
	flag.StringVar(&dashboardurl, "dashboardurl", "noauth", "Dashboard url to use for authentication. Skip authentication when not set.")

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

	if dashboardurl != "noauth" {
		namespaces, err := auth.AuthenticateAndGetNamespaces(dashboardurl, deployment.Email, deployment.Password)

		if err != nil {
			logger.Println("Could not authenticate: ", err)
			return
		}

		if !auth.StringInSet(deployment.Namespace, namespaces) {
			logger.Printf("User %v not authorised to namespace %v", "admin@amdatu.org", deployment.Namespace)
			return
		}
	}

	deployer := cluster.NewDeployer(kubernetesurl, etcdUrl, deployment, &logger)
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
			deploymentError = bluegreen.NewBlueGreen(deployer).Deploy();
		case "rolling":
			deploymentError = rolling.NewRollingDeployer(deployer).Deploy();
		default:
			deploymentError = errors.New(fmt.Sprintf("Unknown type of deployment: %v", deployment.DeploymentType))
		}
	} else {
		logger.Println("Existing service found. Using redeployer")
		deploymentError = redeploy.NewRedeployer(deployer).Deploy();
	}

	if  deploymentError != nil {
		logger.Printf("Error during deployment: %v\n", deploymentError)
		logger.Println("============================ Deployment Failed =======================")
	} else {
		logger.Println("============================ Completed deployment =======================")
	}



}

