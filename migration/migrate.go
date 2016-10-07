package migration

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/coreos/etcd/Godeps/_workspace/src/golang.org/x/net/context"
	etcdclient "github.com/coreos/etcd/client"
)

type deployment struct {
	Ns  string
	Id  string
	App string
	Ts  string
	Pod string
}

var etcdUrl string

func Migrate(url string) error {

	etcdUrl = url
	return migrateIds()

}

func migrateIds() error {

	cfg := etcdclient.Config{
		Endpoints: []string{etcdUrl},
	}

	etcdClient, err := etcdclient.New(cfg)
	if err != nil {
		return errors.New("could not connect to etcd for migration")
	}
	etcdApi := etcdclient.NewKeysAPI(etcdClient)

	// check if migration was done already by looking for new "descriptors" directory
	_, err = etcdApi.Get(context.Background(), "deployment/descriptors", nil)
	if err != nil {
		if strings.Contains(err.Error(), "Key not found") {
			fmt.Println("migrating deployment ids...")
		} else {
			return errors.New("could not read migration done marker: " + err.Error())
		}
	} else {
		fmt.Println("deployment id migration already done")
		return nil
	}

	deployments, err := etcdApi.Get(context.Background(), "/deployment", &etcdclient.GetOptions{Recursive: true})
	if err != nil {
		return errors.New("could not read deployments: " + err.Error())
	}

	for _, namespace := range deployments.Node.Nodes {
		fmt.Println("migrating: " + namespace.Key)

		if strings.HasSuffix(namespace.Key, "healthlog") {
			fmt.Println("  handling healthlogs")
			for _, ns := range namespace.Nodes {
				fmt.Println("    ns: " + ns.Key)
				for _, app := range ns.Nodes {
					fmt.Println("      app: " + app.Key)
					for _, ts := range app.Nodes {
						fmt.Println("        ts: " + ts.Key)
						for _, pod := range ts.Nodes {
							fmt.Println("          migrating pod: " + pod.Key)
							d := parseHealthKey(pod.Key)
							// save with appname in key, delete old with id in key
							etcdApi.Set(context.Background(), "/deployment/healthlog/"+d.Ns+"/"+d.App+"/"+d.Ts+"/"+d.Pod, pod.Value, nil)
							etcdApi.Delete(context.Background(), "/deployment/healthlog/"+d.Ns+"/"+d.Id, &etcdclient.DeleteOptions{Recursive: true, Dir: true})
						}
					}
				}
			}
		} else {
			var ns string
			for _, app := range namespace.Nodes {
				fmt.Println("      app: " + app.Key)
				for _, ts := range app.Nodes {
					fmt.Println("        migrating ts: " + ts.Key)
					d := parseDeploymentKey(ts.Key)
					ns = d.Ns
					// save with appname in key, delete old with id in key
					etcdApi.Set(context.Background(), "/deployment/descriptors/"+d.Ns+"/"+d.App+"/"+d.Ts, ts.Value, nil)
					etcdApi.Delete(context.Background(), "/deployment/"+d.Ns+"/"+d.Id, &etcdclient.DeleteOptions{Recursive: true, Dir: true})
				}
			}
			etcdApi.Delete(context.Background(), "/deployment/"+ns, &etcdclient.DeleteOptions{Recursive: true, Dir: true})
		}

	}

	return nil

}

func parseHealthKey(key string) deployment {
	// "/deployment/healthlog/namespace/id/date/pod"
	parts := strings.Split(key, "/")
	appname := extractAppnameFromId(parts[4])
	deployment := deployment{parts[3], parts[4], appname, parts[5], parts[6]}
	json, _ := json.Marshal(deployment)
	fmt.Println("            parsed: " + string(json))
	return deployment
}

func parseDeploymentKey(key string) deployment {
	// "/deployment/namespace/id/date"
	parts := strings.Split(key, "/")
	appname := extractAppnameFromId(parts[3])
	deployment := deployment{parts[2], parts[3], appname, parts[4], ""}
	json, _ := json.Marshal(deployment)
	fmt.Println("            parsed: " + string(json))
	return deployment
}

func extractAppnameFromId(id string) string {
	// "app-name-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
	regex := regexp.MustCompile("(.*)-.{8}-.{4}-.{4}-.{4}-.{12}")
	return regex.FindAllStringSubmatch(id, 1)[0][1]
}
