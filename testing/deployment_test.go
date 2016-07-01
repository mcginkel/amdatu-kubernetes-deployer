package testing

import (
	"testing"
	"flag"
	"os"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/cluster"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-go/api/v1"
	"net/http"
	"encoding/json"
	"bytes"
	"strings"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-go/client"
	etcdclient "github.com/coreos/etcd/client"
	"golang.org/x/net/context"
	"log"
	"time"
	"strconv"
	"fmt"
)

var (
	deployerUrl = flag.String("deployer", "", "Deployer API url")
	kubernetesUrl = flag.String("kubernetes", "", "kubernetes API url")
	etcdUrl = flag.String("etcd", "", "etcd cluster urls")
	nrOfConcurrentRuns = flag.Int("concurrent", 0, "Number of concurrent deployments to test")
	namespace = flag.String("namespace", "integrationtests", "namespace to test in")
)

const APPNAME = "integrationtest"

var kubernetes client.Client
var etcd etcdclient.KeysAPI


func TestMain(m *testing.M) {
	flag.Parse()

	cfg := etcdclient.Config{
		Endpoints: []string{*etcdUrl},
	}


	etcdClient, err := etcdclient.New(cfg)
	etcd = etcdclient.NewKeysAPI(etcdClient)
	if err != nil {
		panic("Error connecting to etcd: " + err.Error())
	}

	if *kubernetesUrl != "" {

		kubernetes = client.NewClient(*kubernetesUrl, "", "")
		resetEnvironment()

		result := m.Run()

		os.Exit(result)
	}
}

func TestProxyAfterFirstFailedDeployment(t *testing.T) {
	deployment := createDeployment(false, true, false)
	result, err := startDeploy(deployment)

	if err != nil {
		t.Fatal(err)
	}

	if isDeploymentSuccessfull(result) {
		t.Error("Health check failed, but deployment was successful")
	}

	_, err = etcd.Get(context.Background(), "/proxy/frontends/deployer-" + *namespace + ".cloudrti.com", &etcdclient.GetOptions{})
	if err == nil {
		t.Error("Proxy frontend not deleted")
	}

	checkNoReclicationController(t)
}

func deploySuccessful(t *testing.T) {
	deployment := createDeployment(true, true, false)
	result, err := startDeploy(deployment)

	if err != nil {
		t.Fatal(err)
	}

	if !isDeploymentSuccessfull(result) {
		t.Error("Failed deployment")
	}

	nrOfPods := countPodsForApp(t)
	if nrOfPods != 2 {
		t.Errorf("Incorrect number of pods found: %v", nrOfPods)
	}

	checkProxyConfig(t, "")
}

func TestConsecutiveDeployments(t *testing.T) {
	deploySuccessful(t)

	version := getReplicationControllerVersion(t)

	deploySuccessful(t)
	checkReplicationControllers(version, t)


}

func TestFailedHealthCheck(t *testing.T) {
	deployment := createDeployment(false, true, false)
	result, err := startDeploy(deployment)

	if err != nil {
		t.Fatal(err)
	}

	if isDeploymentSuccessfull(result) {
		t.Error("Health check failed, but deployment was successful")
	}
}

func TestIgnoreFailedHealthCheck(t *testing.T) {
	deployment := createDeployment(false, true, true)
	result, err := startDeploy(deployment)

	if err != nil {
		t.Fatal(err)
	}

	if !isDeploymentSuccessfull(result) {
		t.Error("Health check should be ignored, but deployment failed")
	}
}

func TestConcurrentDeploy(t *testing.T) {
	deployment := createDeployment(true, false, false)

	results := make(chan bool)

	for i := 0; i < *nrOfConcurrentRuns; i++ {
		fmt.Printf("Spinning up deployment: %v\n", i)
		go backgroundDeploy(deployment, results)
	}

	for i := 0; i < *nrOfConcurrentRuns; i++ {
		success := <-results
		if !success {
			t.Error("Unexpected failed deployment")
		}
	}

	nrOfPods := countPodsForApp(t)
	if nrOfPods != 2 {
		t.Errorf("Incorrect number of pods found: %v", nrOfPods)
	}

	checkProxyConfig(t, "")
}

func TestDeployWithoutHealthCheck(t *testing.T) {
	deployment := &cluster.Deployment{
		DeploymentType: "blue-green",
		NewVersion: "#",
		AppName: "nginx",
		Replicas: 2,
		PodSpec: v1.PodSpec{
			Containers: []v1.Container{{
				Name: "nginx",
				Image: "nginx",
				Ports: []v1.ContainerPort{{
					ContainerPort: 80,
				}},
			},
			},
		},
		UseHealthCheck: false,
		Namespace: *namespace,
	}

	result, err := startDeploy(deployment)

	if err != nil {
		t.Fatal(err)
	}

	if !isDeploymentSuccessfull(result) {
		t.Error("Failed deployment")
	}

	nrOfPods := countPodsForApp(t)
	if nrOfPods != 2 {
		t.Errorf("Incorrect number of pods found: %v", nrOfPods)
	}
}

func TestRedeployShouldFail(t *testing.T) {

	labels := make(map[string] string)
	labels["app"] = "nginx"
	rcList, err := kubernetes.ListReplicationControllersWithLabel(*namespace, labels)

	if err != nil {
		t.Fatal("Incorrect numer of replication controllers")
	}

	if len(rcList.Items) == 0 {
		t.Fatal("Incorrect numer of replication controllers")
	}

	if len(rcList.Items) > 1 {
		t.Fatal("Incorrect numer of replication controllers")
	}

	version := rcList.Items[0].Labels["version"]

	deployment := &cluster.Deployment{
		DeploymentType: "blue-green",
		NewVersion: version,
		AppName: "nginx",
		Replicas: 2,
		PodSpec: v1.PodSpec{
			Containers: []v1.Container{{
				Name: "nginx",
				Image: "nginx",
				Ports: []v1.ContainerPort{{
					ContainerPort: 80,
				}},
			},
			},
		},
		UseHealthCheck: false,
		Namespace: *namespace,
	}

	result, err := startDeploy(deployment)

	if err != nil {
		t.Fatal(err)
	}

	if isDeploymentSuccessfull(result) {
		t.Error("Deploying the same version again should fail the deployment")
	}
}

func resetEnvironment() {

	rcList, _ := kubernetes.ListReplicationControllers(*namespace)
	for _, rc := range rcList.Items {
		kubernetes.Patch(*namespace, "replicationcontrollers", rc.Name, `{"spec": {"replicas": 0}}`)
	}

	err := kubernetes.DeleteNamespace(*namespace)

	if err != nil {
		log.Println("Namespace not deleted")
	}

	for {
		foundTestNamespace := false

		namespaces, err := kubernetes.ListNamespaces()
		if err != nil {
			log.Fatal("Error listing namespaces", err)
		}

		for _, ns := range namespaces.Items {
			if ns.Name == *namespace {
				foundTestNamespace = true
			}
		}

		if foundTestNamespace {
			time.Sleep(1 * time.Second)
		} else {
			break;
		}
	}

	_,err = kubernetes.CreateNamespace(*namespace)
	if err != nil {
		log.Fatal("Error creating namespace: %v", err)
	}

	etcd.Delete(context.Background(), "/proxy/frontends/deployer-" + *namespace + ".cloudrti.com", &etcdclient.DeleteOptions{})
}


func checkProxyConfig(t *testing.T, version string) {
	resp, err := etcd.Get(context.Background(), "/proxy/frontends/deployer-" + *namespace + ".cloudrti.com", &etcdclient.GetOptions{})
	if err != nil {
		t.Error(err)
		return
	}

	value := resp.Node.Value
	fr := frontend{}

	if err = json.Unmarshal([]byte(value), &fr); err != nil {
		t.Error(err)
	}

	if fr.Hostname != "deployer-" + *namespace +".cloudrti.com" {
		t.Errorf("Hostname not set correctly: %v", fr.Hostname)
	}

	rcList, err := kubernetes.ListReplicationControllers(*namespace)
	if err != nil || len(rcList.Items) != 1 {
		t.Fatal("Error listing replication controllers")
	}

	if fr.BackendId != *namespace + "-" + rcList.Items[0].Name {
		t.Error("Incorrect proxy backend: " + fr.BackendId)
	}

}

func countPodsForApp(t *testing.T) int {
	labels := map[string]string {"app": APPNAME}

	pods, err := kubernetes.ListPodsWithLabel(*namespace, labels)
	if err != nil {
		t.Error(err)
	}

	nrOfRunning := 0
	for _,pod := range pods.Items {
		if pod.Status.Phase == "Running" {
			println(pod.Name)
			nrOfRunning++
		}
	}

	return nrOfRunning
}

func backgroundDeploy(deployment *cluster.Deployment, resultChan chan bool) {
	result, err := startDeploy(deployment)
	if err != nil {
		resultChan <- false
		return
	}

	resultChan <- isDeploymentSuccessfull(result)
}

func getReplicationControllerVersion(t *testing.T) int {
	rcList, err := kubernetes.ListReplicationControllers(*namespace)
	if err != nil {
		t.Fatal("Error listing replication controllers")
	}

	if len(rcList.Items) == 0 {
		return 0
	}

	if len(rcList.Items) > 1 {
		t.Fatal("Incorrect numer of replication controllers")
	}

	versionString := rcList.Items[0].Labels["version"]
	version,_ := strconv.Atoi(versionString)

	return version
}

func checkNoReclicationController(t *testing.T) {
	rcList, err := kubernetes.ListReplicationControllers(*namespace)
	if err != nil {
		t.Fatal("Error retrieving Replication Controllers")
	}

	if len(rcList.Items) != 0 {
		t.Error("Invalid number of replication controllers")
	}
}

func checkReplicationControllers(previousVersion int, t *testing.T) {
	rcList, err := kubernetes.ListReplicationControllers(*namespace)
	if err != nil {
		t.Fatal("Error retrieving Replication Controllers")
	}

	if len(rcList.Items) != 1 {
		t.Error("Invalid number of replication controllers")
	}

	versionString := rcList.Items[0].Labels["version"]
	version,_ := strconv.Atoi(versionString)

	if version != previousVersion +1 {
		t.Error("Invalid version for replication controller")
	}
}

func isDeploymentSuccessfull(log string) bool {
	return strings.Contains(log, "Completed deployment")
}

func startDeploy(deployment *cluster.Deployment) (string, error) {
	buf, err := json.Marshal(deployment)
	if err != nil {
		return "", err
	}

	jsonInputReader := bytes.NewReader(buf)


	resp, err := http.Post(*deployerUrl, "application/json", jsonInputReader)

	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	bodyBuf := new(bytes.Buffer)
	bodyBuf.ReadFrom(resp.Body)
	return bodyBuf.String(), nil

}

func createDeployment(healthy bool, useHealthCheck bool, ignoreHealthCheck bool) *cluster.Deployment {
	var tag string
	if healthy {
		tag = "healthy"
	} else {
		tag = "unhealthy"
	}

	return &cluster.Deployment{
		DeploymentType: "blue-green",
		NewVersion: "#",
		AppName: APPNAME,
		Replicas: 2,
		Frontend: "deployer-" + *namespace + ".cloudrti.com",
		PodSpec: v1.PodSpec{
			Containers: []v1.Container{{
				Name: "deployer-demo",
				Image: "paulbakker/deployer-demo:" + tag,
				Ports: []v1.ContainerPort{{
					ContainerPort: 9999,
				}},
			},
			},
		},
		UseHealthCheck: useHealthCheck,
		Namespace: *namespace,
		IgnoreHealthCheck: ignoreHealthCheck,
	}
}

type frontend struct {
	Hostname string
	Type string
	BackendId string
}

