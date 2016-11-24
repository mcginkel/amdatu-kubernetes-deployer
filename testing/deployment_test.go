package testing

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"errors"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/etcdregistry"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-go/api/v1"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-go/client"
	etcdclient "github.com/coreos/etcd/client"
	"golang.org/x/net/context"
)

var (
	deployerUrl        = flag.String("deployer", "", "Deployer API url")
	kubernetesUrl      = flag.String("kubernetes", "", "kubernetes API url")
	etcdUrl            = flag.String("etcd", "", "etcd cluster urls")
	nrOfConcurrentRuns = flag.Int("concurrent", 0, "Number of concurrent deployments to test")
	namespace          = flag.String("namespace", "integrationtests", "namespace to test in")
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
	descriptor := createDescriptor("probe", false, true, false)
	result, err := startDeploy(descriptor, t)

	if err != nil {
		t.Fatal(err)
	}

	if isDeploymentSuccessfull(result) {
		t.Error("Health check failed, but deployment was successful")
	}

	// wait a bit, cleanup is done after deployment status is set to failure
	time.Sleep(5 * time.Second)

	_, err = etcd.Get(context.Background(), "/proxy/frontends/deployer-"+*namespace+".cloudrti.com", &etcdclient.GetOptions{})
	if err == nil {
		t.Error("Proxy frontend not deleted")
	}

	checkNoReclicationController(t)
}

func deploySuccessful(t *testing.T, healthcheckType string) {
	descriptor := createDescriptor(healthcheckType, true, true, false)
	result, err := startDeploy(descriptor, t)

	if err != nil {
		t.Fatal(err)
	}

	if !isDeploymentSuccessfull(result) {
		t.Fatal("Failed deployment")
	}

	nrOfPods := countPodsForApp(t)
	if nrOfPods != 2 {
		t.Errorf("Incorrect number of pods found, expected 2, found %v", nrOfPods)
	}

	checkProxyConfig(t, "")
}

func TestConsecutiveDeployments(t *testing.T) {
	deploySuccessful(t, "probe")

	version := getReplicationControllerVersion(t)

	deploySuccessful(t, "simple")
	checkReplicationControllers(version, t)

}

func TestFailedProbeHealthCheck(t *testing.T) {
	descriptor := createDescriptor("probe", false, true, false)
	result, err := startDeploy(descriptor, t)

	if err != nil {
		t.Fatal(err)
	}

	if isDeploymentSuccessfull(result) {
		t.Error("Health check failed, but deployment was successful")
	}
}

func TestFailedSimpleHealthCheck(t *testing.T) {
	descriptor := createDescriptor("simple", false, true, false)
	result, err := startDeploy(descriptor, t)

	if err != nil {
		t.Fatal(err)
	}

	if isDeploymentSuccessfull(result) {
		t.Error("Health check failed, but deployment was successful")
	}
}

func TestIgnoreFailedHealthCheck(t *testing.T) {
	descriptor := createDescriptor("probe", false, true, true)
	result, err := startDeploy(descriptor, t)

	if err != nil {
		t.Fatal(err)
	}

	if !isDeploymentSuccessfull(result) {
		t.Error("Health check should be ignored, but deployment failed")
	}
}

func TestConcurrentDeploy(t *testing.T) {
	descriptor := createDescriptor("probe", true, false, false)

	results := make(chan bool)

	for i := 0; i < *nrOfConcurrentRuns; i++ {
		fmt.Printf("Spinning up deployment: %v\n", i)
		go backgroundDeploy(descriptor, results, t)
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
	descriptor := &types.Descriptor{
		DeploymentType: "blue-green",
		NewVersion:     "#",
		AppName:        APPNAME,
		Replicas:       2,
		PodSpec: v1.PodSpec{
			Containers: []v1.Container{{
				Name:  "deployer-demo",
				Image: "amdatu/amdatu-kubernetes-deployer-demo:alpha",
				Ports: []v1.ContainerPort{{
					ContainerPort: 9999,
				}},
			},
			},
		},
		UseHealthCheck: false,
		Namespace:      *namespace,
	}

	result, err := startDeploy(descriptor, t)

	if err != nil {
		t.Fatal(err)
	}

	if !isDeploymentSuccessfull(result) {
		t.Error("Failed deployment")
	}

	// wait a bit, containers might still be starting up
	time.Sleep(10 * time.Second)

	nrOfPods := countPodsForApp(t)
	if nrOfPods != 2 {
		t.Errorf("Incorrect number of pods found: %v", nrOfPods)
	}
}

func TestRedeployShouldFail(t *testing.T) {

	labels := make(map[string]string)
	labels["app"] = APPNAME
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

	descriptor := &types.Descriptor{
		DeploymentType: "blue-green",
		NewVersion:     version,
		AppName:        APPNAME,
		Replicas:       2,
		PodSpec: v1.PodSpec{
			Containers: []v1.Container{{
				Name:  "deployer-demo",
				Image: "amdatu/amdatu-kubernetes-deployer-demo:alpha",
				Ports: []v1.ContainerPort{{
					ContainerPort: 9999,
				}},
			},
			},
		},
		UseHealthCheck: false,
		Namespace:      *namespace,
	}

	result, err := startDeploy(descriptor, t)

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

	kubernetes.DeleteNamespace(*namespace)

	for {
		foundTestNamespace := false

		namespaces, _ := kubernetes.ListNamespaces()

		for _, ns := range namespaces.Items {
			if ns.Name == *namespace {
				foundTestNamespace = true
			}
		}

		if foundTestNamespace {
			time.Sleep(1 * time.Second)
		} else {
			break
		}
	}

	kubernetes.CreateNamespace(*namespace)

	etcd.Delete(context.Background(), "/proxy/frontends/deployer-"+*namespace+".cloudrti.com", &etcdclient.DeleteOptions{})

	registry := etcdregistry.NewEtcdRegistry(etcd)
	deployments, err := registry.GetDeployments(*namespace)
	if err == nil {
		for _, deployment := range deployments {
			registry.DeleteDeployment(*namespace, deployment.Id)
		}
	}
	descriptors, err := registry.GetDescriptors(*namespace)
	if err == nil {
		for _, descriptor := range descriptors {
			registry.DeleteDescriptor(*namespace, descriptor.Id)
		}
	}
}

func checkProxyConfig(t *testing.T, version string) {
	resp, err := etcd.Get(context.Background(), "/proxy/frontends/deployer-"+*namespace+".cloudrti.com", &etcdclient.GetOptions{})
	if err != nil {
		t.Error(err)
		return
	}

	value := resp.Node.Value
	fr := frontend{}

	if err = json.Unmarshal([]byte(value), &fr); err != nil {
		t.Error(err)
	}

	if fr.Hostname != "deployer-"+*namespace+".cloudrti.com" {
		t.Errorf("Hostname not set correctly: %v", fr.Hostname)
	}

	rcList, err := kubernetes.ListReplicationControllers(*namespace)
	if err != nil || len(rcList.Items) != 1 {
		t.Fatal("Error listing replication controllers")
	}

	if fr.BackendId != *namespace+"-"+rcList.Items[0].Name {
		t.Error("Incorrect proxy backend: " + fr.BackendId)
	}

}

func countPodsForApp(t *testing.T) int {
	labels := map[string]string{"app": APPNAME}

	pods, err := kubernetes.ListPodsWithLabel(*namespace, labels)
	if err != nil {
		t.Error(err)
	}

	nrOfRunning := 0
	for _, pod := range pods.Items {
		t.Logf("podstatus of %v is %v", pod.Name, pod.Status.Phase)
		if pod.Status.Phase == "Running" {
			nrOfRunning++
		}
	}

	return nrOfRunning
}

func backgroundDeploy(descriptor *types.Descriptor, resultChan chan bool, t *testing.T) {
	result, err := startDeploy(descriptor, t)
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
	version, _ := strconv.Atoi(versionString)

	return version
}

func checkNoReclicationController(t *testing.T) {
	rcList, err := kubernetes.ListReplicationControllers(*namespace)
	if err != nil {
		t.Fatal("Error retrieving Replication Controllers")
	}

	if len(rcList.Items) != 0 {
		t.Fatal("Invalid number of replication controllers, expected 0, got " + strconv.Itoa(len(rcList.Items)))
	}
}

func checkReplicationControllers(previousVersion int, t *testing.T) {
	rcList, err := kubernetes.ListReplicationControllers(*namespace)
	if err != nil {
		t.Fatal("Error retrieving Replication Controllers")
	}

	if len(rcList.Items) != 1 {
		t.Fatal("Invalid number of replication controllers, expected 1, got " + strconv.Itoa(len(rcList.Items)))
	}

	versionString := rcList.Items[0].Labels["version"]
	version, _ := strconv.Atoi(versionString)

	if version != previousVersion+1 {
		t.Error("Invalid version for replication controller, expected " + strconv.Itoa(previousVersion+1) + ", got " + strconv.Itoa(version))
	}
}

func isDeploymentSuccessfull(status string) bool {
	return status == types.DEPLOYMENTSTATUS_DEPLOYED
}

func startDeploy(descriptor *types.Descriptor, t *testing.T) (string, error) {
	buf, err := json.Marshal(descriptor)
	if err != nil {
		return "", err
	}

	jsonInputReader := bytes.NewReader(buf)

	resp, err := http.Post(*deployerUrl+"descriptors", "application/json", jsonInputReader)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 201 {
		return "", errors.New("creating descriptor didn't return 201")
	}
	descLocation := resp.Header.Get("Location")
	lastSep := strings.LastIndex(descLocation, "/")
	descId := descLocation[lastSep+1:]
	lastSep = strings.Index(descId, "?")
	descId = descId[:lastSep]

	t.Log("descriptor id: " + descId)

	resp, err = http.Post(fmt.Sprintf("%vdeployments?namespace=%v&descriptorId=%v", *deployerUrl, *namespace, descId), "application/json", strings.NewReader(""))
	if err != nil {
		return "", err
	}

	if resp.StatusCode != 202 {
		return "", errors.New("creating deployment didn't return 202")
	}

	deplLocation := resp.Header.Get("Location")
	lastSep = strings.LastIndex(deplLocation, "/")
	deplId := deplLocation[lastSep+1:]
	lastSep = strings.Index(deplId, "?")
	deplId = deplId[:lastSep]

	t.Log("deployment id: " + deplId)

	deploying := true
	deployment := &types.Deployment{}
	for deploying {
		resp, err = http.Get(*deployerUrl + deplLocation)
		defer resp.Body.Close()
		bodyBuf := new(bytes.Buffer)
		bodyBuf.ReadFrom(resp.Body)
		err = json.Unmarshal(bodyBuf.Bytes(), deployment)
		if err != nil {
			return "", errors.New("error parsing deployment: " + err.Error())
		}
		if deployment.Status == types.DEPLOYMENTSTATUS_DEPLOYING {
			time.Sleep(2 * time.Second)
		} else {
			deploying = false
		}

	}

	t.Log("deployment version: " + deployment.Version)

	return deployment.Status, nil
}

func createDescriptor(healthcheckType string, healthy bool, useHealthCheck bool, ignoreHealthCheck bool) *types.Descriptor {
	var path string
	if healthy && strings.EqualFold(healthcheckType, "probe") {
		path = "healthy"
	} else if !healthy && strings.EqualFold(healthcheckType, "probe") {
		path = "unhealthy"
	} else if healthy && strings.EqualFold(healthcheckType, "simple") {
		path = "simple"
	} else if !healthy && strings.EqualFold(healthcheckType, "simple") {
		path = "error"
	}

	return &types.Descriptor{
		DeploymentType: "blue-green",
		NewVersion:     "#",
		AppName:        APPNAME,
		Replicas:       2,
		Frontend:       "deployer-" + *namespace + ".cloudrti.com",
		PodSpec: v1.PodSpec{
			Containers: []v1.Container{{
				Name:  "deployer-demo",
				Image: "amdatu/amdatu-kubernetes-deployer-demo:alpha",
				Ports: []v1.ContainerPort{{
					ContainerPort: 9999,
				}},
			},
			},
		},
		UseHealthCheck:    useHealthCheck,
		HealthCheckPath:   path,
		HealthCheckType:   healthcheckType,
		HealthCheckPort:   9999,
		Namespace:         *namespace,
		IgnoreHealthCheck: ignoreHealthCheck,
	}
}

type frontend struct {
	Hostname  string
	Type      string
	BackendId string
}
