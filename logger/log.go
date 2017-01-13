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
package logger

import (
	"fmt"
	"log"

	"strings"
	"time"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/etcdregistry"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
)

type Logger interface {
	Println(v ...interface{})
	Printf(format string, v ...interface{})
}

type ConsoleLogger struct{}

func NewConsoleLogger() *ConsoleLogger {
	return &ConsoleLogger{}
}

func (logger *ConsoleLogger) Println(v ...interface{}) {
	msg := fmt.Sprintln(v...)
	log.Println(msg)
}

func (logger *ConsoleLogger) Printf(format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	log.Println(msg)
}

type DeploymentLogger struct {
	deployment *types.Deployment
	registry   *etcdregistry.EtcdRegistry
	baseLogger Logger
}

func NewDeploymentLogger(deployment *types.Deployment, registry *etcdregistry.EtcdRegistry, baseLogger Logger) *DeploymentLogger {
	return &DeploymentLogger{deployment, registry, baseLogger}
}

func (logger *DeploymentLogger) Println(v ...interface{}) {
	logger.baseLogger.Println(v)
	msg := fmt.Sprintln(v...)
	logger.addToDeployment(msg)
}

func (logger *DeploymentLogger) Printf(format string, v ...interface{}) {
	logger.baseLogger.Printf(format, v)
	msg := fmt.Sprintf(format, v...)
	logger.addToDeployment(msg)
}

func (logger *DeploymentLogger) addToDeployment(msg string) {
	logger.registry.StoreLogLine(logger.deployment.Descriptor.Namespace, logger.deployment.Id, addTimeAndNewLine(msg))
}

func addTimeAndNewLine(msg string) string {
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	ts := time.Now().Format(time.RFC3339)
	msg = ts + " " + msg
	return msg
}
