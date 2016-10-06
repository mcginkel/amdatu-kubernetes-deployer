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
	"strings"
	"sync"
	"time"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/auth"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/bluegreen"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/cluster"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/deploymentregistry"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/environment"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/migration"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/undeploy"
	etcdclient "github.com/coreos/etcd/client"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"golang.org/x/net/context"
)

var kubernetesurl, etcdUrl, port, authurl, kubernetesUsername, kubernetesPassword, proxyRestUrl string
var healthTimeout int64
var proxyReloadSleep int
var mutex = &sync.Mutex{}
var namespaceMutexes = map[string]*sync.Mutex{}

type deploymentStatus struct {
	Success   bool   `json:"success"`
	Ts        string `json:"ts,omitempty"`
	Podstatus string `json:"podstatus,omitempty"`
}

func init() {
	flag.StringVar(&kubernetesurl, "kubernetes", "", "URL to the Kubernetes API server")
	flag.StringVar(&etcdUrl, "etcd", "", "Url to etcd")
	flag.StringVar(&port, "deployport", "8000", "Port to listen for deployments")
	flag.StringVar(&authurl, "authurl", "noauth", "Url to use for authentication. Skip authentication when not set.")
	flag.StringVar(&kubernetesUsername, "kubernetesusername", "noauth", "Username to authenticate against Kubernetes API server. Skip authentication when not set")
	flag.StringVar(&kubernetesPassword, "kubernetespassword", "noauth", "Username to authenticate against Kubernetes API server.")
	flag.Int64Var(&healthTimeout, "timeout", 60, "Timeout in seconds for health checks")
	flag.IntVar(&proxyReloadSleep, "proxysleep", 20, "Seconds to wait for proxy to reload config")
	flag.StringVar(&proxyRestUrl, "proxyrest", "", "Proxy REST url")

	exampleUsage := "Missing required argument %v. Example usage: ./deployer_linux_amd64 -kubernetes http://[kubernetes-api-url]:8080 -etcd http://[etcd-url]:2379 -deployport 8000"

	flag.Parse()

	if kubernetesurl == "" {
		log.Fatalf(exampleUsage, "kubernetes")
	}

	if etcdUrl == "" {
		log.Fatalf(exampleUsage, "etcd")
	}
}

func main() {

	if err := migration.Migrate(etcdUrl); err != nil {
		log.Fatal(err)
	}

	r := mux.NewRouter()
	r.HandleFunc("/deployments/{namespace}", listDeployments).Methods("GET")
	r.HandleFunc("/deployments/history/{namespace}/{appname}", deleteDeploymentHistoryHandler).Methods("DELETE")
	r.HandleFunc("/deployments/{namespace}/{appname}", undeploymentHandler).Methods("DELETE")
	r.HandleFunc("/deployment", deploymentHandler).Methods("POST")
	r.HandleFunc("/redeployment/{namespace}/{appname}/{ts}", redeploymentHandler).Methods("POST")

	r.HandleFunc("/validate", validationHandler).Methods("POST")

	r.HandleFunc("/deployment/stream", deployWebsocketHandler)
	r.HandleFunc("/undeployment/stream/{namespace}/{appname}", undeployWebsocketHandler)

	r.HandleFunc("/healthcheckdata", healthcheckDataHandler)

	fmt.Printf("Deployer starting and listening on port %v\n", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatal(err)
	}

}

func healthcheckDataHandler(w http.ResponseWriter, r *http.Request) {
	cfg := etcdclient.Config{
		Endpoints: []string{etcdUrl},
	}

	etcdClient, err := etcdclient.New(cfg)
	if err != nil {
		log.Println("Couldn't connect to etcd")
	}

	etcdApi := etcdclient.NewKeysAPI(etcdClient)
	dirPath := r.URL.Query().Get("dir")
	dirPath = strings.Replace(dirPath, " ", "+", -1)
	dir, err := etcdApi.Get(context.Background(), dirPath, &etcdclient.GetOptions{Recursive: true})

	log.Printf("Getting directory %v from etcd\n", dirPath)

	if err != nil {
		log.Println(err)
		return
	}

	result := []string{}

	for _, node := range dir.Node.Nodes {
		result = append(result, node.Value)
	}

	jsonStr, err := json.Marshal(result)
	if err != nil {
		log.Println(err)
		return
	}

	w.Write(jsonStr)
}

func listDeployments(w http.ResponseWriter, r *http.Request) {

	logger := logger.NewHttpLogger(w)
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

func deleteDeploymentHistoryHandler(w http.ResponseWriter, r *http.Request) {

	logger := logger.NewHttpLogger(w)
	defer logger.Flush()

	registry, err := createDeploymentRegistry(logger)
	if err != nil {
		w.WriteHeader(500)
		return
	}

	vars := mux.Vars(r)

	err = registry.DeleteDeployment(vars["namespace"], vars["appname"])

	if err != nil {
		w.WriteHeader(500)
		io.WriteString(w, "Error deleting deployment history: "+err.Error())
		return
	}
}

func createDeploymentRegistry(logger logger.Logger) (*deploymentregistry.DeploymentRegistry, error) {
	cfg := etcdclient.Config{
		Endpoints: []string{etcdUrl},
	}

	etcdClient, err := etcdclient.New(cfg)
	if err != nil {
		logger.Println("Error connecting to etcd: " + err.Error())
		return nil, err
	}

	registry := deploymentregistry.NewDeploymentRegistry(etcdClient)
	return &registry, nil
}

func createEnvironmentVarStore(logger logger.Logger) (*environment.EnvironmentVarStore, error) {
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

	logger := logger.NewWebsocketLogger(conn)

	_, body, err := conn.ReadMessage()
	if err != nil {
		logger.Printf("Error reading body: %v", err)
	}

	deployment, err := createDeployment(body)
	if err != nil {
		logger.Printf("Error parsing body: %v", err)
	}

	err = deploy(&deployment, logger)
	keyName := fmt.Sprintf("/deployment/healthlog/%v/%v/%v", deployment.Namespace, deployment.AppName, deployment.DeploymentTs)

	status := &deploymentStatus{}
	if err == nil {
		status.Success = true
		status.Ts = deployment.DeploymentTs
	} else {
		status.Success = false
	}
	status.Podstatus = keyName
	statusBytes, _ := json.Marshal(status)
	statusString := "!!" + string(statusBytes)

	if !status.Success {
		logger.Printf("Error during deployment: %v\n", err)
		logger.Println("============================ Deployment Failed =======================")
		logger.Println(statusString) // this is parsed by the frontend!
	} else {
		logger.Println("============================ Completed deployment =======================")
		logger.Println(statusString) // this is parsed by the frontend!
	}

	conn.WriteMessage(websocket.CloseMessage, []byte{})
}

func deploymentHandler(responseWriter http.ResponseWriter, req *http.Request) {

	logger := logger.NewHttpLogger(responseWriter)
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

	err = deploy(&deployment, logger)
	if err != nil {
		responseWriter.WriteHeader(500)
		logger.Printf("Error during deployment: %v\n", err)
		logger.Println("============================ Deployment Failed =======================")
	} else {
		logger.Println("============================ Completed deployment =======================")
	}

}

func redeploymentHandler(w http.ResponseWriter, r *http.Request) {

	logger := logger.NewHttpLogger(w)
	defer logger.Flush()

	vars := mux.Vars(r)
	namespace := vars["namespace"]
	appname := vars["appname"]
	timestamp := vars["ts"]

	registry, err := createDeploymentRegistry(logger)
	if err != nil {
		w.WriteHeader(500)
		return
	}

	deployment, err := registry.FindDeployment(namespace, appname, timestamp)
	if err != nil {
		w.WriteHeader(500)
		io.WriteString(w, "Error finding deployments: "+err.Error())
		return
	}

	err = deploy(&deployment, logger)
	if err != nil {
		w.WriteHeader(500)
		logger.Printf("Error during deployment: %v\n", err)
		logger.Println("============================ Redeployment Failed =======================")
	} else {
		logger.Println("============================ Completed redeployment =======================")
	}

}

func validationHandler(responseWriter http.ResponseWriter, req *http.Request) {

	logger := logger.NewHttpLogger(responseWriter)
	defer logger.Flush()

	defer req.Body.Close()
	body, err := ioutil.ReadAll(req.Body)

	if err != nil {
		logger.Printf("Error reading body: %v", err)
	} else {
		deployment, err := createDeployment(body)
		if err != nil {
			logger.Printf("Error parsing body: %v", err)
		} else {
			err = deployment.SetDefaults().Validate()
			if err != nil {
				logger.Printf("Invalid deployment descriptor: %v", err)
			}
		}
	}
}

func undeployWebsocketHandler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	defer conn.Close()
	if err != nil {
		log.Println(err)
		return
	}

	logger := logger.NewWebsocketLogger(conn)

	_, body, err := conn.ReadMessage()
	if err != nil {
		logger.Printf("Error reading body: %v", err)
	}

	user, err := createUser(body)
	if err != nil {
		logger.Printf("Error parsing body: %v", err)
	}

	vars := mux.Vars(r)

	err = unDeploy(vars["namespace"], vars["appname"], user.Email, user.Password, logger)

	status := &deploymentStatus{}
	status.Success = err == nil
	statusBytes, _ := json.Marshal(status)
	statusString := "!!" + string(statusBytes)

	if !status.Success {
		logger.Printf("Error during undeployment: %v\n", err)
		logger.Println("============================ Undeployment Failed =======================")
		logger.Println(statusString) // this is parsed by the frontend!
	} else {
		logger.Println("============================ Completed Undeployment =======================")
		logger.Println(statusString) // this is parsed by the frontend!
	}

	conn.WriteMessage(websocket.CloseMessage, []byte{})

}

func undeploymentHandler(w http.ResponseWriter, req *http.Request) {

	logger := logger.NewHttpLogger(w)
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

	err = unDeploy(vars["namespace"], vars["appname"], user.Email, user.Password, logger)
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

func deploy(deployment *types.Deployment, logger logger.Logger) error {
	if _, ok := namespaceMutexes[deployment.Namespace]; !ok {
		namespaceMutexes[deployment.Namespace] = &sync.Mutex{}
	}

	logger.Printf("Trying to acquire mutex for namesapce %v\n", deployment.Namespace)
	namespaceMutexes[deployment.Namespace].Lock()
	defer namespaceMutexes[deployment.Namespace].Unlock()

	logger.Printf("Acquired mutex for namesapce %v\n", deployment.Namespace)

	deploymentTs := time.Now().Format(time.RFC3339)
	deployment.DeploymentTs = deploymentTs

	if err := deployment.SetDefaults().Validate(); err != nil {
		logger.Printf("Deployment descriptor incorrect: \n %v", err.Error())
		return err
	}

	logger.Printf("%v\n", deployment.String())

	if err := authorize(deployment.Namespace, deployment.Email, deployment.Password, logger); err != nil {
		return err
	}

	cfg := etcdclient.Config{
		Endpoints: []string{etcdUrl},
	}

	etcdClient, err := etcdclient.New(cfg)
	if err != nil {
		log.Fatal("Couldn't connect to etcd")
	}

	registry := deploymentregistry.NewDeploymentRegistry(etcdClient)

	deployer := cluster.NewDeployer(kubernetesurl, kubernetesUsername, kubernetesPassword, etcdClient, deployment, logger, healthTimeout, proxyRestUrl, proxyReloadSleep)
	if deployment.DeployedVersion == "000" {
		rc, err := deployer.FindCurrentRc()
		if err != nil || len(rc) == 0 {
			deployer.Deployment.DeployedVersion = "1"
		} else if len(rc) > 1 {
			return errors.New("Could not determine next deployment version, more than a singe Replication Controller found")
		} else {
			for _, ctrl := range rc {
				logger.Println(ctrl.Name)
				versionString := ctrl.Labels["version"]
				newVersion, err := cluster.DetermineNewVersion(versionString)
				if err != nil {
					logger.Printf("Could not determine next deployment version based on current version %v", err.Error())

					return err
				} else {
					logger.Printf("New deployment version: %v", newVersion)
					deployer.Deployment.DeployedVersion = newVersion
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
		return errors.New("Existing service found, this version is already deployed. Exiting deployment.")
	}

	var deploymentLog string
	if deploymentError == nil {
		deploymentLog = "success"
	} else {
		deploymentLog = deploymentError.Error()
	}

	result := types.DeploymentResult{}
	result.Date = deploymentTs
	result.Status = deploymentLog
	result.Deployment = *deployment

	registry.StoreDeployment(result)

	if deploymentError != nil {
		deployer.CleanupFailedDeployment()
		return deploymentError
	}

	return nil
}

func createDeployment(jsonString []byte) (types.Deployment, error) {
	deployment := types.Deployment{}

	if err := json.Unmarshal(jsonString, &deployment); err != nil {
		return types.Deployment{}, err
	}

	return deployment, nil
}

func createUser(jsonString []byte) (types.User, error) {
	user := types.User{}

	if err := json.Unmarshal(jsonString, &user); err != nil {
		return types.User{}, err
	}

	return user, nil
}

func unDeploy(namespace string, appname string, email string, password string, logger logger.Logger) error {

	mutex.Lock()
	defer mutex.Unlock()

	if err := authorize(namespace, email, password, logger); err != nil {
		return err
	}

	undeployer, err := undeploy.NewUndeployer(namespace, appname, etcdUrl, kubernetesurl, kubernetesUsername, kubernetesPassword, logger, proxyRestUrl, proxyReloadSleep)

	if err != nil {
		return err
	}

	return undeployer.Undeploy()
}

func authorize(namespace string, email string, password string, logger logger.Logger) error {
	if authurl != "noauth" {
		namespaces, err := auth.AuthenticateAndGetNamespaces(authurl, email, password)

		if err != nil {
			logger.Println("Could not authenticate: ", err)
			return err
		}

		if !auth.StringInSet(namespace, namespaces) {
			logger.Printf("User %v not authorized to namespace %v", email, namespace)
			return errors.New("Not authorized for namespace")
		}
	}
	return nil
}
