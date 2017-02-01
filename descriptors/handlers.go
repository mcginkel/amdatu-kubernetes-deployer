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
package descriptors

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/etcdregistry"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/helper"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	"github.com/gorilla/mux"
	"github.com/satori/go.uuid"
)

type DescriptorHandlers struct {
	registry *etcdregistry.EtcdRegistry
}

func NewDescriptorHandlers(registry *etcdregistry.EtcdRegistry) *DescriptorHandlers {
	return &DescriptorHandlers{registry}
}

func (d *DescriptorHandlers) CreateDescriptorHandler(writer http.ResponseWriter, req *http.Request) {

	logger := logger.NewConsoleLogger()

	logger.Println("Creating descriptor")

	//TODO check namespaces of user
	namespace := req.URL.Query().Get("namespace")
	if namespace == "" {
		helper.HandleError(writer, logger, 400, "Namespace parameter missing")
		return
	}

	defer req.Body.Close()
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		helper.HandleError(writer, logger, 500, "Error reading body: %v", err)
		return
	}

	descriptor, err := CreateDescriptor(body)
	if err != nil {
		helper.HandleError(writer, logger, 400, "Error parsing body: %v", err)
		return
	}

	if descriptor.Namespace != namespace {
		helper.HandleError(writer, logger, 400, "Descriptor namespace does not match namespace parameter")
		return
	}

	if descriptor.Id != "" {
		helper.HandleError(writer, logger, 400, "Id must not be set")
		return
	}

	//TODO check namespaces of user

	descriptor.Id = uuid.NewV4().String()

	err = descriptor.SetDefaults().Validate()
	if err != nil {
		helper.HandleError(writer, logger, 400, "Invalid descriptor: %v", err)
		return
	}

	err = d.registry.CreateDescriptor(descriptor)
	if err != nil {
		helper.HandleError(writer, logger, 500, "Error storing descriptor: %v", err)
		return
	}

	helper.HandleCreated(writer, logger, "/descriptors/"+descriptor.Id+"?namespace="+descriptor.Namespace, "Descriptor created: %v", descriptor.Id)
}

func (d *DescriptorHandlers) DoValidationHandler(writer http.ResponseWriter, req *http.Request) {
	logger := logger.NewConsoleLogger()

	defer req.Body.Close()
	body, err := ioutil.ReadAll(req.Body)

	if err != nil {
		helper.HandleError(writer, logger, 500, "Error reading body: %v", err)
		return
	} else {
		descriptor, err := CreateDescriptor(body)
		if err != nil {
			helper.HandleError(writer, logger, 200, "Error: could not parse descriptor: %v", err)
			return
		} else {
			err = descriptor.SetDefaults().Validate()
			if err != nil {
				helper.HandleError(writer, logger, 200, "Error: invalid descriptor: %v", err)
				return
			}
		}
	}
	helper.HandleSuccess(writer, logger, "", "Descriptor valid")
}

func (d *DescriptorHandlers) ListDescriptorsHandler(writer http.ResponseWriter, req *http.Request) {

	logger := logger.NewConsoleLogger()

	//TODO check namespaces of user
	namespace := req.URL.Query().Get("namespace")
	if namespace == "" {
		helper.HandleError(writer, logger, 400, "Namespace parameter missing")
		return
	}

	logger.Printf("Listing descriptors for namespace %v", namespace)

	descriptors, err := d.registry.GetDescriptors(namespace)
	if (err != nil && err == etcdregistry.ErrDescriptorNotFound) || len(descriptors) == 0 {
		helper.HandleNotFound(writer, logger, "No descriptors found for namespace %v\n", namespace)
		return
	} else if err != nil {
		helper.HandleError(writer, logger, 500, "Error getting deployments for namespace %v: %v", namespace, err)
		return
	}

	// filter for appname if needed
	appname := req.URL.Query().Get("appname")
	if appname != "" {
		var newDescriptors []*types.Descriptor
		for _, descriptor := range descriptors {
			if descriptor.AppName == appname {
				newDescriptors = append(newDescriptors, descriptor)
			}
		}
		if len(newDescriptors) == 0 {
			helper.HandleNotFound(writer, logger, "No descriptors found for namespace %v and appname %v\n", namespace, appname)
			return
		}
		descriptors = newDescriptors
	}

	helper.HandleSuccess(writer, logger, descriptors, "Successfully listed descriptors")
}

func (d *DescriptorHandlers) GetDescriptorHandler(writer http.ResponseWriter, req *http.Request) {
	logger := logger.NewConsoleLogger()

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

	descriptor, err := GetDescriptorById(d.registry, namespace, id, logger)
	if err == etcdregistry.ErrDescriptorNotFound {
		helper.HandleNotFound(writer, logger, "Descriptor %v not found", id)
		return
	} else if err != nil {
		helper.HandleError(writer, logger, 500, "Error getting descriptor %v: %v", id, err)
		return
	}

	helper.HandleSuccess(writer, logger, descriptor, "Descriptor %v found", id)
}

func (d *DescriptorHandlers) UpdateDescriptorHandler(writer http.ResponseWriter, req *http.Request) {
	logger := logger.NewConsoleLogger()

	logger.Println("Updating descriptor")

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

	defer req.Body.Close()
	body, err := ioutil.ReadAll(req.Body)
	if err != nil {
		helper.HandleError(writer, logger, 500, "Error reading body: %v", err)
		return
	}

	descriptor, err := CreateDescriptor(body)
	if err != nil {
		helper.HandleError(writer, logger, 400, "Error parsing body: %v", err)
		return
	}

	if descriptor.Id == "" {
		helper.HandleError(writer, logger, 400, "Descriptor id must be set for updates")
		return
	}
	if descriptor.Id != id {
		helper.HandleError(writer, logger, 400, "Descriptor id does not match id parameter")
		return
	}
	if descriptor.Namespace != namespace {
		helper.HandleError(writer, logger, 400, "Descriptor namespace does not match namespace parameter")
		return
	}

	// get old descriptor in order to check if update is allowed
	oldDescriptor, err := d.registry.GetDescriptorById(descriptor.Namespace, id)
	if err != nil {
		helper.HandleError(writer, logger, 400, "Could not get find old descriptor in namespace %v with id %v", descriptor.Namespace, id)
		return
	}
	if oldDescriptor.AppName != descriptor.AppName {
		helper.HandleError(writer, logger, 400, "AppName update is not allowed!")
		return
	}

	err = d.registry.UpdateDescriptor(descriptor)
	if err != nil {
		helper.HandleError(writer, logger, 500, "Error updating descriptor: %v", err)
		return
	}

	helper.HandleSuccess(writer, logger, "", "Descriptor updated: %v", descriptor.Id)
}

func (d *DescriptorHandlers) DeleteDescriptorHandler(writer http.ResponseWriter, req *http.Request) {
	logger := logger.NewConsoleLogger()

	logger.Println("Deleting descriptor")

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

	_, err := d.registry.GetDescriptorById(namespace, id)
	if err != nil && err != etcdregistry.ErrDescriptorNotFound {
		helper.HandleError(writer, logger, 500, "Error getting descriptor for namespace %v with id %v: %v", namespace, id, err)
		return
	} else if err == etcdregistry.ErrDescriptorNotFound {
		helper.HandleNotFound(writer, logger, "Descriptor %v not found.", id)
		return
	}

	err = d.registry.DeleteDescriptor(namespace, id)
	if err != nil {
		helper.HandleError(writer, logger, 500, "Error deleting descriptor for namespace %v with id %v: %v", namespace, id, err)
		return
	}

	helper.HandleSuccess(writer, logger, "", "Descriptor %v deleted.", id)
}

func CreateDescriptor(jsonString []byte) (*types.Descriptor, error) {
	descriptor := &types.Descriptor{}

	if err := json.Unmarshal(jsonString, descriptor); err != nil {
		return &types.Descriptor{}, err
	}

	return descriptor, nil
}

func GetDescriptorById(registry *etcdregistry.EtcdRegistry, namespace string, id string, logger logger.Logger) (*types.Descriptor, error) {
	logger.Printf("Getting descriptor %v\n", id)

	descriptor, err := registry.GetDescriptorById(namespace, id)
	if err != nil && err != etcdregistry.ErrDescriptorNotFound {
		return &types.Descriptor{}, errors.New(fmt.Sprintf("Error getting descriptor for namespace %v with id %v: %v\n", namespace, id, err))
	} else if err == etcdregistry.ErrDescriptorNotFound {
		return &types.Descriptor{}, err
	}

	return descriptor, nil
}
