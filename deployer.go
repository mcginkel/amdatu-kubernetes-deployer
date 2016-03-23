package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"

	"com.amdatu.rti.deployment/auth"
	"com.amdatu.rti.deployment/bluegreen"
	"com.amdatu.rti.deployment/cluster"
	"com.amdatu.rti.deployment/deploymentregistry"
	"com.amdatu.rti.deployment/redeploy"
	"com.amdatu.rti.deployment/undeploy"
	etcdclient "github.com/coreos/etcd/client"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/satori/go.uuid"
)

var kubernetesurl, etcdUrl, port, authurl, kubernetesUsername, kubernetesPassword, kafkaUrl, influxUrl, influxUser, influxPassword string
var mutex = &sync.Mutex{}

func init() {
	flag.StringVar(&kubernetesurl, "kubernetes", "", "URL to the Kubernetes API server")
	flag.StringVar(&etcdUrl, "etcd", "", "Url to etcd")
	flag.StringVar(&port, "deployport", "8000", "Port to listen for deployments")
	flag.StringVar(&authurl, "authurl", "noauth", "Url to use for authentication. Skip authentication when not set.")
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
	r.HandleFunc("/deployments/{namespace}", listDeployments).Methods("GET")
	r.HandleFunc("/deployments/history/{namespace}/{id}", deleteDeploymentHistory).Methods("DELETE")
	r.HandleFunc("/deployments/{namespace}/{appname}", UndeploymentHandler).Methods("DELETE")
	r.HandleFunc("/deployment", DeploymentHandler).Methods("POST")

	fmt.Printf("Dployer started and listening on port %v\n", port)

	r.HandleFunc("/deployment/stream", deployWebsocketHandler)
	r.HandleFunc("/undeployment/stream/{namespace}/{appname}", undeployWebsocketHandler)

	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal(err)
	}
}

func listDeployments(w http.ResponseWriter, r *http.Request) {

	logger := cluster.NewHttpLogger(w)
	defer logger.Flush()

	registry, err := createDeploymentRegistry(logger)
	if err != nil {
		w.WriteHeader(500)
		return
	}

	vars := mux.Vars(r)
	deployments, err := registry.ListDeployments(vars["namespace"])

	if err != nil {
		w.WriteHeader(500)
		io.WriteString(w, "Error listing deployments: "+err.Error())
		return
	}

	jsonStr, err := json.MarshalIndent(deployments, "", "   ")
	if err != nil {
		w.WriteHeader(500)
		io.WriteString(w, "Error listing deployments: "+err.Error())
		return
	}

	w.Write(jsonStr)
}

func deleteDeploymentHistory(w http.ResponseWriter, r *http.Request) {

	logger := cluster.NewHttpLogger(w)
	defer logger.Flush()

	registry, err := createDeploymentRegistry(logger)
	if err != nil {
		w.WriteHeader(500)
		return
	}

	vars := mux.Vars(r)

	err = registry.DeleteDeployment(vars["namespace"], vars["id"])

	if err != nil {
		w.WriteHeader(500)
		io.WriteString(w, "Error storing deployment: "+err.Error())
		return
	}
}

func createDeploymentRegistry(logger cluster.Logger) (*deploymentregistry.DeploymentRegistry, error) {
	cfg := etcdclient.Config{
		Endpoints: []string{etcdUrl},
	}

	etcdClient, err := etcdclient.New(cfg)
	if err != nil {
		logger.Println("Error connecting to etcd: " + err.Error())
		return nil, err
	}

	registry := deploymentregistry.NewDeploymentRegistry(&etcdClient)
	return &registry, nil
}

func createEnvironmentVarStore(logger cluster.Logger) (*environment.EnvironmentVarStore, error) {
	cfg := etcdclient.Config{
		Endpoints: []string{etcdUrl},
	}

	etcdClient, err := etcdclient.New(cfg)
	if err != nil {
		logger.Printf("Error connecting to etcd: %v\n", err.Error())
		return &environment.EnvironmentVarStore{}, err
	}

	store := environment.NewEnvironmentVarStore(etcdClient, logger)
	return store, nil
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func deployWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	defer conn.Close()
	if err != nil {
		log.Println(err)
		return
	}

	logger := cluster.NewWebsocketLogger(conn)

	_, body, err := conn.ReadMessage()
	if err != nil {
		logger.Printf("Error reading body: %v", err)
	}

	deployment, err := createDeployment(body)
	if err != nil {
		logger.Printf("Error parsing body: %v", err)
	}

	err = deploy(&deployment, &logger)
	if err != nil {
		logger.Printf("Error during deployment: %v\n", err)
		logger.Println("============================ Deployment Failed =======================")
		logger.Println("!!{\"success\": \"false\"}") // this is parsed by the frontend!
	} else {
		logger.Println("============================ Completed deployment =======================")
		logger.Println("!!{\"success\": \"true\", \"id\": \"" + deployment.Id + "\"}") // this is parsed by the frontend!
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

	environment.NewEnvironmentVarStore(nil, logger)

	deployment, err := createDeployment(body)
	if err != nil {
		logger.Printf("Error parsing body: %v", err)
	}

	err = deploy(&deployment, &logger)
	if err != nil {
		responseWriter.WriteHeader(500)
		logger.Printf("Error during deployment: %v\n", err)
		logger.Println("============================ Deployment Failed =======================")
	} else {
		logger.Println("============================ Completed deployment =======================")
	}

}

func undeployWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	defer conn.Close()
	if err != nil {
		log.Println(err)
		return
	}

	logger := cluster.NewWebsocketLogger(conn)

	registry, err := createDeploymentRegistry(logger)
	if err != nil {
		return
	}

	defer r.Body.Close()
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		logger.Printf("Error reading body: %v", err)
	}

	user, err := createUser(body)
	if err != nil {
		logger.Printf("Error parsing user: %v", err)
	}

	vars := mux.Vars(r)

	err = unDeploy(vars["namespace"], vars["appname"], user.Email, user.Password, logger)
	if err != nil {
		logger.Printf("Error during undeployment: %v\n", err)
		logger.Println("============================ Deployment Failed =======================")
		logger.Println("!!{\"success\": \"false\"}") // this is parsed by the frontend!
	} else {
		err2 := registry.DeleteDeployment(vars["namespace"], vars["id"])
		if err2 != nil {
			logger.Printf("Error during deleting history: %v\n", err2)
		}
		logger.Println("============================ Completed deployment =======================")
		logger.Println("!!{\"success\": \"true\"}") // this is parsed by the frontend!
	}
}

func UndeploymentHandler(w http.ResponseWriter, req *http.Request) {

	logger := cluster.NewHttpLogger(w)
	defer logger.Flush()

	defer req.Body.Close()
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		logger.Printf("Error reading body: %v", err)
	}

	user, err := createUser(body)
	if err != nil {
		logger.Printf("Error parsing user: %v", err)
	}

	vars := mux.Vars(req)

	err = unDeploy(vars["namespace"], vars["appname"], user.Email, user.Password, &logger)
	if err != nil {
		w.WriteHeader(500)
		logger.Printf("Error during undeployment: %v\n", err)
		logger.Println("============================ Undeployment Failed =======================")
	} else {
		logger.Println("============================ Completed Undeployment =======================")
	}

}

type DeploymentRequest struct {
	ResponseWriter http.ResponseWriter
	Req            *http.Request
}

func deploy(deployment *cluster.Deployment, logger cluster.Logger) error {
	mutex.Lock()
	defer mutex.Unlock()

	if err := deployment.SetDefaults().Validate(); err != nil {
		logger.Printf("Deployment descriptor incorrect: \n %v", err.Error())
		return err
	}

	logger.Printf("%v\n", deployment.String())

	if err := authenticate(deployment.Namespace, deployment.Email, deployment.Password, logger); err != nil {
		return err;
	}
	
	deployer := cluster.NewDeployer(kubernetesurl, kubernetesUsername, kubernetesPassword, etcdUrl, *deployment, logger)
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

	envVarStore, err := createEnvironmentVarStore(logger)
	if err != nil {
		return err
	}

	deployer.Deployment.Environment = envVarStore.GetEnvironmentVars()

	var deploymentError error

	/*Check if namespace has the current version deployed
	If so, switch to redeployer
	*/

	logger.Println("Checking for existing service...")
	_, err = deployer.K8client.GetService(deployment.Namespace, deployer.CreateRcName())

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

	if deployment.Id == "" {
		id := uuid.NewV1().String()
		deployment.Id = deployment.AppName + "-" + string(id)
	}

	var deploymentLog string
	if deploymentError == nil {
		deploymentLog = "success"
	} else {
		deploymentLog = deploymentError.Error()
	}

	result := cluster.DeploymentResult{}
	result.Date = time.Now().Format(time.RFC3339)
	result.Status = deploymentLog
	result.Deployment = *deployment

	registry := deploymentregistry.NewDeploymentRegistry(deployer.EtcdClient)
	registry.StoreDeployment(result)

	if deploymentError != nil {
		deployer.CleanupFailedDeployment()
		return err
	}

	return nil
}

func createDeployment(jsonString []byte) (cluster.Deployment, error) {
	deployment := cluster.Deployment{}

	if err := json.Unmarshal(jsonString, &deployment); err != nil {
		return cluster.Deployment{}, err
	}

	return deployment, nil
}

func createUser(jsonString []byte) (cluster.User, error) {
	user := cluster.User{}

	if err := json.Unmarshal(jsonString, &user); err != nil {
		return cluster.User{}, err
	}

	return user, nil
}

func unDeploy(namespace string, appname string, email string, password string, logger cluster.Logger) error {

	mutex.Lock()
	defer mutex.Unlock()

	if err := authenticate(namespace, email, password, logger); err != nil {
		return err;
	}

	undeployer, err := undeploy.NewUndeployer(namespace, appname, etcdUrl, kubernetesurl, kubernetesUsername, kubernetesPassword, logger)
	
	if err != nil {
		return err;
	}
	
	return undeployer.Undeploy()
}

func authenticate(namespace string, email string, password string, logger cluster.Logger) error {
	if dashboardurl != "noauth" {
		namespaces, err := auth.AuthenticateAndGetNamespaces(dashboardurl, email, password)

		if err != nil {
			logger.Println("Could not authenticate: ", err)
			return err
		}

		if !auth.StringInSet(namespace, namespaces) {
			logger.Printf("User %v not authorised to namespace %v", email, namespace)
			return errors.New("Not authorized for namespace")
		}
	}
	return nil;
}

