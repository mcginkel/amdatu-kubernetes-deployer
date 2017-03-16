/*
Copyright (c) 2016 The Amdatu Foundation

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package deployments

import (
	"encoding/json"
	"net/http"

	"fmt"

	"time"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/descriptors"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/etcdregistry"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/helper"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/satori/go.uuid"
)

type DeploymentHandlers struct {
	registry *etcdregistry.EtcdRegistry
	config   helper.DeployerConfig
}

func NewDeploymentHandlers(config helper.DeployerConfig) *DeploymentHandlers {
	return &DeploymentHandlers{config.EtcdRegistry, config}
}

func (d *DeploymentHandlers) CreateDeploymentHandler(writer http.ResponseWriter, req *http.Request) {
	myLogger := logger.NewConsoleLogger()
	myLogger.Println("Creating deployment")

	//TODO check namespaces of user
	namespace := req.URL.Query().Get("namespace")
	if namespace == "" {
		helper.HandleError(writer, myLogger, 400, "Namespace parameter missing")
		return
	}

	descriptorId := req.URL.Query().Get("descriptorId")
	if descriptorId == "" {
		helper.HandleError(writer, myLogger, 400, "DescriptorId parameter missing")
		return
	}

	descriptor, err := descriptors.GetDescriptorById(d.registry, namespace, descriptorId, myLogger)
	if err == etcdregistry.ErrDescriptorNotFound {
		helper.HandleError(writer, myLogger, 400, "Descriptor %v not found", descriptorId)
		return
	} else if err != nil {
		helper.HandleError(writer, myLogger, 500, "Error getting descriptor %v: %v", descriptorId, err)
		return
	}

	if namespace != descriptor.Namespace {
		helper.HandleError(writer, myLogger, 400, "Namespaces of request parameter and descriptor do not match!")
		return
	}

	//TODO check namespaces of user

	d.deploy(writer, req, descriptor, myLogger)
}

func (d *DeploymentHandlers) ListDeploymentsHandler(writer http.ResponseWriter, req *http.Request) {

	logger := logger.NewConsoleLogger()

	//TODO check namespaces of user
	namespace := req.URL.Query().Get("namespace")
	if namespace == "" {
		helper.HandleError(writer, logger, 400, "Namespace parameter missing")
		return
	}

	logger.Printf("Listing deployments for namespace %v", namespace)

	deployments, err := d.registry.GetDeployments(namespace)
	if (err != nil && err == etcdregistry.ErrDeploymentNotFound) || len(deployments) == 0 {
		helper.HandleNotFound(writer, logger, "No deployments found for namespace %v\n", namespace)
		return
	} else if err != nil {
		helper.HandleError(writer, logger, 500, "Error getting deployments for namespace %v: %v", namespace, err)
		return
	}

	// filter for appname if needed
	appname := req.URL.Query().Get("appname")
	if appname != "" {
		var newDeployments []*types.Deployment
		for _, deployment := range deployments {
			if deployment.Descriptor.AppName == appname {
				newDeployments = append(newDeployments, deployment)
			}
		}
		if len(newDeployments) == 0 {
			helper.HandleNotFound(writer, logger, "No deployments found for namespace %v and appname %v\n", namespace, appname)
			return
		}
		deployments = newDeployments
	}

	helper.HandleSuccess(writer, logger, deployments, "Successfully listed deployments")
}

func (d *DeploymentHandlers) GetDeploymentHandler(writer http.ResponseWriter, req *http.Request) {
	logger := logger.NewConsoleLogger()
	logger.Println("Getting deployment")

	//TODO check namespaces of user
	namespace := req.URL.Query().Get("namespace")
	if namespace == "" {
		helper.HandleError(writer, logger, 400, "Namespace parameter missing")
		return
	}

	vars := mux.Vars(req)
	id := vars["id"]
	if id == "" {
		helper.HandleError(writer, logger, 400, "Id parameter missing")
		return
	}

	deployment, err := d.getDeployment(namespace, id, logger)
	if err != nil && err != etcdregistry.ErrDeploymentNotFound {
		helper.HandleError(writer, logger, 500, "Error getting deployment with id %v: %v", id, err)
		return
	} else if err == etcdregistry.ErrDeploymentNotFound {
		helper.HandleNotFound(writer, logger, "Deployment %v not found.", id)
		return
	}

	helper.HandleSuccess(writer, logger, deployment, "Deployment %v found.", id)
}

func (d *DeploymentHandlers) GetHealthcheckDataHandler(writer http.ResponseWriter, req *http.Request) {

	logger := logger.NewConsoleLogger()

	namespace := req.URL.Query().Get("namespace")
	vars := mux.Vars(req)
	id := vars["id"]
	if namespace == "" || id == "" {
		helper.HandleError(writer, logger, 400, "Namespace or deploymentId missing")
		return
	}

	logger.Printf("Getting healthcheckdata for namespace %v and id %v\n", namespace, id)

	health, err := d.registry.GetHealth(namespace, id)
	if err != nil {
		helper.HandleNotFound(writer, logger, "Error getting health: %v", err.Error())
		return
	}

	helper.HandleSuccess(writer, logger, health, "Got healthcheckdata successfully")
}

func (d *DeploymentHandlers) GetLogsHandler(writer http.ResponseWriter, req *http.Request) {

	logger := logger.NewConsoleLogger()

	namespace := req.URL.Query().Get("namespace")
	vars := mux.Vars(req)
	id := vars["id"]
	if namespace == "" || id == "" {
		helper.HandleError(writer, logger, 400, "Namespace or deploymentId missing")
		return
	}

	logger.Printf("Getting logs for namespace %v and id %v\n", namespace, id)

	logs, _, err := d.registry.GetLogs(namespace, id)
	if err != nil {
		helper.HandleNotFound(writer, logger, "Error getting logs: %v", err.Error())
		return
	}

	helper.HandleSuccess(writer, logger, logs, "Got logs successfully")
}

func (d *DeploymentHandlers) StreamLogsHandler(writer http.ResponseWriter, req *http.Request) {

	logger := logger.NewConsoleLogger()

	namespace := req.URL.Query().Get("namespace")
	vars := mux.Vars(req)
	id := vars["id"]
	if namespace == "" || id == "" {
		helper.HandleError(writer, logger, 400, "Namespace or deploymentId missing")
		return
	}

	logger.Printf("Streaming logs for namespace %v and id %v\n", namespace, id)

	conn, err := helper.Upgrader.Upgrade(writer, req, nil)
	if err != nil {
		helper.HandleError(writer, logger, 500, "Webcocket upgrade failed")
		return
	}
	defer conn.Close()
	defer conn.WriteMessage(websocket.CloseMessage, []byte{})

	logs, index, err := d.registry.GetLogs(namespace, id)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("!!error Error getting logs: %v", err.Error())))
		return
	}
	conn.WriteMessage(websocket.TextMessage, []byte(logs))

	isBusy, err := d.isBusy(namespace, id, logger)
	if err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("!!error Error getting deployment status: %v", err.Error())))
		return
	}
	for isBusy {
		if err != nil {
			conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("!!error Error getting deployment status: %v", err.Error())))
			return
		}

		var newLogs string
		newLogs, index, err = d.registry.NextLogs(namespace, id, index)
		if err != nil {
			logger.Printf("error getting logs, ignoring...:" + err.Error())
			//conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("!!error Error getting logs: %v", err.Error())))
			//return
		}

		// only write new part of logs
		if len(newLogs) > len(logs) {

			conn.WriteMessage(websocket.TextMessage, []byte(newLogs[len(logs):]))
			logs = newLogs

			time.Sleep(500 * time.Millisecond)

		}

		isBusy, err = d.isBusy(namespace, id, logger)
		if err != nil {
			logger.Printf("error getting deployment status, ignoring...:" + err.Error())
			//conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("!!error Error getting deployment status: %v", err.Error())))
			//return
		}

		time.Sleep(500 * time.Millisecond)
	}
}

func (d *DeploymentHandlers) isBusy(namespace string, id string, logger logger.Logger) (bool, error) {
	deployment, err := d.getDeployment(namespace, id, logger)
	if err != nil {
		return false, err
	}
	return deployment.Status == types.DEPLOYMENTSTATUS_DEPLOYING || deployment.Status == types.DEPLOYMENTSTATUS_UNDEPLOYING, nil
}

func (d *DeploymentHandlers) UpdateDeploymentHandler(writer http.ResponseWriter, req *http.Request) {
	logger := logger.NewConsoleLogger()
	logger.Println("Redeploying deployment")

	//TODO check namespaces of user
	namespace := req.URL.Query().Get("namespace")
	if namespace == "" {
		helper.HandleError(writer, logger, 400, "Namespace parameter missing")
		return
	}

	vars := mux.Vars(req)
	id := vars["id"]
	if id == "" {
		helper.HandleError(writer, logger, 400, "Missing id")
		return
	}
	deployment, err := d.getDeployment(namespace, id, logger)

	if err != nil && err != etcdregistry.ErrDeploymentNotFound {
		helper.HandleError(writer, logger, 500, "Error getting deployment with id %v: %v", id, err)
		return
	} else if err == etcdregistry.ErrDeploymentNotFound {
		helper.HandleNotFound(writer, logger, "Deployment %v not found.", id)
		return
	}

	d.deploy(writer, req, deployment.Descriptor, logger)
}

func (d *DeploymentHandlers) DeleteDeploymentHandler(writer http.ResponseWriter, req *http.Request) {
	var myLogger logger.Logger
	myLogger = logger.NewConsoleLogger()
	myLogger.Println("Deleting deployment")

	//TODO check namespaces of user
	namespace := req.URL.Query().Get("namespace")
	if namespace == "" {
		helper.HandleError(writer, myLogger, 400, "Namespace parameter missing")
		return
	}

	vars := mux.Vars(req)
	id := vars["id"]
	if id == "" {
		helper.HandleError(writer, myLogger, 400, "Missing id")
		return
	}

	deployment, err := d.registry.GetDeploymentById(namespace, id)
	if err != nil && err != etcdregistry.ErrDeploymentNotFound {
		helper.HandleError(writer, myLogger, 500, "Error getting deployment for namespace %v with id %v: %v", namespace, id, err)
		return
	} else if err == etcdregistry.ErrDeploymentNotFound {
		helper.HandleNotFound(writer, myLogger, "Deployment %v not found.", id)
		return
	}

	myLogger = logger.NewDeploymentLogger(deployment, d.config.EtcdRegistry, myLogger)
	d.undeploy(writer, req, deployment, myLogger)

}

func (d *DeploymentHandlers) createDeployment(jsonString []byte) (*types.Deployment, error) {
	deployment := &types.Deployment{}

	if err := json.Unmarshal(jsonString, deployment); err != nil {
		return &types.Deployment{}, err
	}

	return deployment, nil
}

func (d *DeploymentHandlers) deploy(writer http.ResponseWriter, req *http.Request, descriptor *types.Descriptor, myLogger logger.Logger) {

	deployment := &types.Deployment{}
	deployment.Descriptor = descriptor
	deployment.Id = uuid.NewV4().String()
	deployment.SetVersion()
	deployment.Status = types.DEPLOYMENTSTATUS_DEPLOYING

	err := d.registry.CreateDeployment(deployment)
	if err != nil {
		helper.HandleError(writer, myLogger, 500, "Error storing deployment: %v", err)
		return
	}

	myLogger = logger.NewDeploymentLogger(deployment, d.config.EtcdRegistry, myLogger)
	myLogger.Println("Deployment id: " + deployment.Id)

	deployer := NewDeployer(d.config)

	// start deployment async
	go deployer.deploy(deployment, myLogger)

	helper.HandleStarted(writer, myLogger, "/deployments/"+deployment.Id+"/?namespace="+descriptor.Namespace, "Deployment started: %v", deployment.Id)
}

func (d *DeploymentHandlers) undeploy(writer http.ResponseWriter, req *http.Request, deployment *types.Deployment, myLogger logger.Logger) {

	deleteDeployment := req.URL.Query().Get("deleteDeployment") == "true"

	undeployer := NewUndeployer(d.config)

	// start undeployment async
	go undeployer.Undeploy(deployment, myLogger, deleteDeployment)

	helper.HandleStarted(writer, myLogger, "/deployments/"+deployment.Id, "Undeployment started, namespace %v, appname %v\n", deployment.Descriptor.Namespace, deployment.Descriptor.AppName)
}

func (d *DeploymentHandlers) getDeployment(namespace string, id string, logger logger.Logger) (*types.Deployment, error) {
	logger.Printf("Getting deployment %v\n", id)

	deployment, err := d.registry.GetDeploymentById(namespace, id)
	if err != nil {
		return &types.Deployment{}, err
	}
	return deployment, nil
}
