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

	for _, item := range deployments.Node.Nodes {
		fmt.Println("migrating: " + item.Key)

		if strings.HasSuffix(item.Key, "healthlog") {
			fmt.Println("  handling healthlogs")
			if item.Dir {
				for _, namespace := range item.Nodes {
					fmt.Println("    ns: " + namespace.Key)
					if namespace.Dir {
						for _, app := range namespace.Nodes {
							fmt.Println("      app: " + app.Key)
							d := parseHealthKey(app.Key)
							ns := d.Ns
							id := d.Id
							if app.Dir {
								for _, ts := range app.Nodes {
									fmt.Println("        ts: " + ts.Key)
									if ts.Dir {
										for _, pod := range ts.Nodes {
											fmt.Println("          migrating pod: " + pod.Key)
											d := parseHealthKey(pod.Key)
											// save with appname in key, delete old with id in key
											_, err = etcdApi.Set(context.Background(), "/deployment/healthlog/"+d.Ns+"/"+d.App+"/"+d.Ts+"/"+d.Pod, pod.Value, nil)
											if err != nil {
												fmt.Println("SET ERROR: " + err.Error())
											}
										}
									}
								}
							}
							_, err = etcdApi.Delete(context.Background(), "/deployment/healthlog/"+ns+"/"+id, &etcdclient.DeleteOptions{Recursive: true, Dir: true})
							if err != nil {
								fmt.Println("DELETE ERROR: " + err.Error())
							}
						}
					}
				}
			}
		} else {
			ns := parseDeploymentKey(item.Key).Ns
			if item.Dir {
				for _, app := range item.Nodes {
					fmt.Println("      app: " + app.Key)
					if app.Dir {
						for _, ts := range app.Nodes {
							fmt.Println("        migrating ts: " + ts.Key)
							d := parseDeploymentKey(ts.Key)
							// save with appname in key, delete old with id in key
							_, err = etcdApi.Set(context.Background(), "/deployment/descriptors/"+d.Ns+"/"+d.App+"/"+d.Ts, ts.Value, nil)
							if err != nil {
								fmt.Println("SET ERROR: " + err.Error())
							}
						}
					}
				}
			}
			_, err = etcdApi.Delete(context.Background(), "/deployment/"+ns, &etcdclient.DeleteOptions{Recursive: true, Dir: true})
			if err != nil {
				fmt.Println("DELETE ERROR: " + err.Error())
			}
		}

	}

	return nil

}

func parseHealthKey(key string) deployment {
	// "/deployment/healthlog/namespace/id/date/pod"
	parts := strings.Split(key, "/")
	if len(parts) == 5 {
		return deployment{parts[3], parts[4], "", "", ""}
	}
	appname := extractAppnameFromId(parts[4])
	deployment := deployment{parts[3], parts[4], appname, parts[5], parts[6]}
	json, _ := json.Marshal(deployment)
	fmt.Println("            parsed: " + string(json))
	return deployment
}

func parseDeploymentKey(key string) deployment {
	// "/deployment/namespace/id/date"
	parts := strings.Split(key, "/")
	if len(parts) == 3 {
		return deployment{parts[2], "", "", "", ""}
	}
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
