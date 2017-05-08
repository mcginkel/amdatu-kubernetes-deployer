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
package helper

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	"github.com/gorilla/websocket"
)

var Upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

func HandleSuccess(writer http.ResponseWriter, logger logger.Logger, body interface{}, msg string, args ...interface{}) {

	var bodyBytes []byte
	if bodyString, isString := body.(string); isString {
		bodyBytes = []byte(bodyString)
	} else if healthData, isHealthData := body.([]types.HealthData); isHealthData {
		// last else block does not work for arrays, so we need to cast first implicitly
		// see https://github.com/golang/go/wiki/InterfaceSlice
		var err error
		bodyBytes, err = json.MarshalIndent(healthData, "", "  ")
		if err != nil {
			HandleError(writer, logger, 500, "Error marshalling result to json: %v", err)
			return
		}
	} else {
		var err error
		bodyBytes, err = json.MarshalIndent(body, "", "  ")
		if err != nil {
			HandleError(writer, logger, 500, "Error marshalling result to json: %v", err)
			return
		}
	}

	if len(bodyBytes) == 0 {
		writer.WriteHeader(204)
	} else {
		// writes 200 status automatically
		writer.Write(bodyBytes)
	}

	logMsg(logger, msg, args...)
}

func HandleCreated(writer http.ResponseWriter, logger logger.Logger, location string, msg string, args ...interface{}) {
	handleNew(writer, logger, location, 201, msg, args)
}

func HandleStarted(writer http.ResponseWriter, logger logger.Logger, location string, msg string, args ...interface{}) {
	handleNew(writer, logger, location, 202, msg, args)
}

func handleNew(writer http.ResponseWriter, logger logger.Logger, location string, status int, msg string, args ...interface{}) {
	writer.Header().Set("Location", location)
	writer.WriteHeader(status)
	logMsg(logger, msg, args...)
}

func HandleError(writer http.ResponseWriter, logger logger.Logger, status int, msg string, args ...interface{}) {
	message := fmt.Sprintf(msg, args...)
	if !strings.HasSuffix(message, "\n") {
		message += "\n"
	}
	writer.WriteHeader(status)
	writer.Write([]byte(message))
	logMsg(logger, msg, args...)
}

func HandleNotFound(writer http.ResponseWriter, logger logger.Logger, msg string, args ...interface{}) {
	writer.WriteHeader(404)
	logMsg(logger, msg, args...)
}

func logMsg(logger logger.Logger, msg string, args ...interface{}) {
	var message string
	if len(args) > 0 {
		message = fmt.Sprintf(msg, args...)
	} else {
		message = msg
	}
	logger.Println(message)
}
