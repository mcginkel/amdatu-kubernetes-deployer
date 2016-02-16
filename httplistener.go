package main

import (
	"github.com/gorilla/mux"
	"com.amdatu.rti.deployment/auth"
	"com.amdatu.rti.deployment/bluegreen"
	"com.amdatu.rti.deployment/cluster"
	"com.amdatu.rti.deployment/deploymentregistry"
	"com.amdatu.rti.deployment/redeploy"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/gorilla/websocket"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
)

var kubernetesurl, etcdUrl, port, dashboardurl, kubernetesUsername, kubernetesPassword, kafkaUrl, influxUrl, influxUser, influxPassword string
var mutex = &sync.Mutex{}

func init() {
	flag.StringVar(&kubernetesurl, "kubernetes", "", "URL to the Kubernetes API server")
	flag.StringVar(&etcdUrl, "etcd", "", "Url to etcd")
	flag.StringVar(&port, "deployport", "8000", "Port to listen for deployments")
	flag.StringVar(&dashboardurl, "dashboardurl", "noauth", "Dashboard url to use for authentication. Skip authentication when not set.")
	flag.StringVar(&kubernetesUsername, "kubernetesusername", "noauth", "Username to authenticate against Kubernetes API server. Skip authentication when not set")
	flag.StringVar(&kubernetesPassword, "kubernetespassword", "noauth", "Username to authenticate against Kubernetes API server.")
	flag.StringVar(&kafkaUrl, "kafka", "", "Kafka url to pass to deployed pods")
	flag.StringVar(&influxUrl, "influx-url", "", "InfluxDB url to pass to deployed pods")
	flag.StringVar(&influxUser, "influx-username", "", "InfluxDB username to pass to deployed pods")
	flag.StringVar(&influxPassword, "influx-password", "", "InfluxDB password to pass to deployed pods")

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

	r.HandleFunc("/deployment/stream", websocketHandler)

	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal(err)
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func websocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	defer conn.Close()
	if err != nil {
		log.Println(err)
		return
	}

	logger := cluster.NewWebsocketLogger(conn)

	_, body, err := conn.ReadMessage()
	if err != nil {
		return
	}

	if err != nil {
		logger.Printf("Error reading body: %v", err)
	}

	deployment, err := createDeployment(body)
	if err != nil {
		logger.Printf("Error parsing body: %v", err)
	}

	err = deploy(deployment, &logger)
	if err != nil {
		logger.Printf("Error during deployment: %v\n", err)
		logger.Println("============================ Deployment Failed =======================")
	} else {
		logger.Println("============================ Completed deployment =======================")
	}

}

func DeploymentHandler(responseWriter http.ResponseWriter, req *http.Request) {

	logger := cluster.NewHttpLogger(responseWriter)
	defer logger.Flush()

	defer req.Body.Close()
	body, err := ioutil.ReadAll(req.Body)

	if err != nil {
		logger.Printf("Error reading body: %v", err)
	}

	deployment, err := createDeployment(body)
	if err != nil {
		logger.Printf("Error parsing body: %v", err)
	}

	err = deploy(deployment, &logger)
	if err != nil {
		responseWriter.WriteHeader(500)
		logger.Printf("Error during deployment: %v\n", err)
		logger.Println("============================ Deployment Failed =======================")
	} else {
		logger.Println("============================ Completed deployment =======================")
	}
}

type DeploymentRequest struct {
	ResponseWriter http.ResponseWriter
	Req            *http.Request
}

func deploy(deployment cluster.Deployment, logger cluster.Logger) error {
	mutex.Lock()
	defer mutex.Unlock()

	if err := deployment.SetDefaults().Validate(); err != nil {
		logger.Printf("Deployment descriptor incorrect: \n %v", err.Error())
		return err
	}

	logger.Printf("%v\n", deployment.String())

	if dashboardurl != "noauth" {
		namespaces, err := auth.AuthenticateAndGetNamespaces(dashboardurl, deployment.Email, deployment.Password)

		if err != nil {
			logger.Println("Could not authenticate: ", err)
			return err
		}

		if !auth.StringInSet(deployment.Namespace, namespaces) {
			logger.Printf("User %v not authorised to namespace %v", "admin@amdatu.org", deployment.Namespace)
			return err
		}
	}

	deployer := cluster.NewDeployer(kubernetesurl, kubernetesUsername, kubernetesPassword, etcdUrl, deployment, logger)
	if deployment.NewVersion == "000" {
		rc, err := deployer.FindCurrentRc()
		if err != nil || len(rc) == 0 {
			deployer.Deployment.NewVersion = "1"
		} else if len(rc) > 1 {
			logger.Println("Could not determine next deployment version, more than a singe Replication Controller found")
			return err
		} else {
			for _, ctrl := range rc {
				logger.Println(ctrl.Name)
				versionString := ctrl.Labels["version"]
				newVersion, err := cluster.DetermineNewVersion(versionString)
				if err != nil {
					logger.Printf("Could not determine next deployment version based on current version %v", err.Error())

					return err
				} else {
					deployer.Deployment.NewVersion = newVersion
				}
			}
		}
	}

	deploymentregistry.NewDeploymentRegistry(deployer.EtcdClient)

	var deploymentError error

	/*Check if namespace has the current version deployed
	If so, switch to redeployer
	*/

	logger.Println("Checking for existing service...")
	_, err := deployer.K8client.GetService(deployment.Namespace, deployer.CreateRcName())

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

		deployer.CleanupFailedDeployment()
		return err
	}

	return nil
}

func createDeployment(jsonString []byte) (cluster.Deployment, error) {
	deployment := cluster.Deployment{Kafka: kafkaUrl, InfluxDbUrl: influxUrl, InfluxDbUser: influxUser, InfluxDbUPassword: influxPassword}

	if err := json.Unmarshal(jsonString, &deployment); err != nil {
		return cluster.Deployment{}, err
	}

	return deployment, nil
}
