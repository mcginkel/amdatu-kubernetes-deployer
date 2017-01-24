package migration

import (
	"encoding/json"
	"errors"
	"strings"

	"log"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/etcdregistry"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	etcdclient "github.com/coreos/etcd/client"
	"github.com/satori/go.uuid"
	"golang.org/x/net/context"
)

type deploymentResult struct {
	Date       string            `json:"date,omitempty"`
	Status     string            `json:"status,omitempty"`
	Descriptor *types.Descriptor `json:"deployment,omitempty"`
}

var etcdApi etcdclient.KeysAPI
var registry *etcdregistry.EtcdRegistry

func Migrate(newEtcdApi etcdclient.KeysAPI, newRegistry *etcdregistry.EtcdRegistry) error {

	etcdApi = newEtcdApi
	registry = newRegistry

	return migrateIds()

}

func migrateIds() error {

	// check if migration was done already by looking for new "descriptors" directory
	_, err := etcdApi.Get(context.Background(), "deployer/descriptors", nil)
	if err != nil {
		if strings.Contains(err.Error(), "Key not found") {
			log.Println("migrating deployments...")
		} else {
			return errors.New("could not read migration done marker: " + err.Error())
		}
	} else {
		log.Println("descriptor / deployment migration already done")
		return nil
	}

	deployments, err := etcdApi.Get(context.Background(), "/deployment/descriptors", &etcdclient.GetOptions{Recursive: true})
	if err != nil {
		log.Println("could not read deployments, assuming fresh etcd installation...")
		return nil
	}

	for _, item := range deployments.Node.Nodes {
		if item.Dir {
			for _, app := range item.Nodes {
				if app.Dir {
					log.Println("migrating: " + app.Key)
					var latestDeploymentResult *deploymentResult
					for _, ts := range app.Nodes {
						deploymentresult := &deploymentResult{}
						json.Unmarshal([]byte(ts.Value), deploymentresult)
						log.Println("  deployment date " + deploymentresult.Date)
						log.Println("  deployment status " + deploymentresult.Status)
						if latestDeploymentResult == nil ||
							(deploymentresult.Status == "success" &&
								strings.Compare(deploymentresult.Date, latestDeploymentResult.Date) == 1) {

							log.Println("    newer successful deployment")
							latestDeploymentResult = deploymentresult
						}
					}

					if latestDeploymentResult == nil {
						log.Println("  NO SUCCESSFUL DEPLOYMENT FOUND!")
						continue
					}

					descriptor := latestDeploymentResult.Descriptor
					descriptor.Id = uuid.NewV4().String()
					descriptor.Created = latestDeploymentResult.Date
					descriptor.LastModified = latestDeploymentResult.Date

					deployment := &types.Deployment{}
					deployment.Id = uuid.NewV3(uuid.NamespaceURL, descriptor.Namespace+"-"+descriptor.AppName+"-"+latestDeploymentResult.Date).String()
					deployment.Version = descriptor.Deprecated_DeployedVersion
					deployment.Created = latestDeploymentResult.Date
					deployment.LastModified = latestDeploymentResult.Date
					deployment.Descriptor = descriptor
					deployment.Status = types.DEPLOYMENTSTATUS_DEPLOYED

					descriptor.Deprecated_DeployedVersion = ""
					descriptor.Deprecated_DeploymentTs = ""

					err = registry.CreateDescriptorWithoutTimestamps(descriptor)
					if err != nil {
						log.Println("  ERROR STORING DESCRIPTOR: " + err.Error())
						continue
					}
					log.Println("    migrated descriptor")
					err = registry.CreateDeploymentWithoutTimestamps(deployment)
					if err != nil {
						log.Println("  ERROR STORING DESCRIPTOR: " + err.Error())
						continue
					}
					log.Println("    migrated deployment")

					healthdata, err := etcdApi.Get(context.Background(), "/deployment/healthlog/"+descriptor.Namespace+"/"+descriptor.AppName+"/"+deployment.Created, &etcdclient.GetOptions{Recursive: true})
					if err != nil {
						log.Println("    could not read healthdata: " + err.Error())
						continue
					}
					for _, poddata := range healthdata.Node.Nodes {
						// /deployment/healthlog/namespace/appname/date/pod
						parts := strings.Split(poddata.Key, "/")
						if len(parts) != 7 {
							log.Println("    could not parse healthdata key")
							continue
						}
						err = registry.StoreHealth(descriptor.Namespace, deployment.Id, parts[6], poddata.Value)
						if err != nil {
							log.Println("    ERROR STORING HEALTH DATA: " + err.Error())
							continue
						}
						log.Println("    also migrated health data")
					}
				}
			}
		}
	}

	return nil

}
