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
package proxies

import (
	"strings"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/k8s"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	"errors"
	"fmt"
	"golang.org/x/net/publicsuffix"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
)

type IngressConfigurator struct {
	k8sClient *k8s.K8sClient
}

func NewIngressConfigurator(k8sClient *k8s.K8sClient) *IngressConfigurator {
	return &IngressConfigurator{
		k8sClient: k8sClient,
	}
}

func (ic *IngressConfigurator) CreateOrUpdateProxy(deployment *types.Deployment,
	service *v1.Service, logger logger.Logger) error {

	descriptor := deployment.Descriptor

	logger.Printf("Getting Ingress for %v", descriptor.AppName)
	ingress, err := ic.k8sClient.GetIngress(descriptor.Namespace, descriptor.AppName)
	if err == nil {

		logger.Println("  found existing Ingress, updating")

		if err := ic.configure(ingress, descriptor, service, logger); err != nil {
			return err
		}

		if _, err := ic.k8sClient.UpdateIngress(descriptor.Namespace, ingress); err != nil {
			return err
		}

	} else if statusError, isStatus := err.(*k8sErrors.StatusError); isStatus && statusError.Status().Reason == meta.StatusReasonNotFound {

		logger.Println("  no Ingress found, creating new one")

		ingress = &v1beta1.Ingress{}

		ingress.Namespace = descriptor.Namespace
		ingress.Name = descriptor.AppName

		annotations := make(map[string]string)
		annotations["kubernetes.io/ingress.class"] = "nginx"
		ingress.Annotations = annotations

		if err := ic.configure(ingress, descriptor, service, logger); err != nil {
			return err
		}

		if _, err := ic.k8sClient.CreateIngress(descriptor.Namespace, ingress); err != nil {
			return err
		}

	} else {
		return err
	}

	return nil
}

func (ic *IngressConfigurator) configure(ingress *v1beta1.Ingress, descriptor *types.Descriptor, service *v1.Service, logger logger.Logger) error {
	if err := ic.setRules(ingress, descriptor, service); err != nil {
		return err
	}
	if err := ic.setTlsConfig(ingress, descriptor, logger); err != nil {
		return err
	}
	return nil
}

func (ic *IngressConfigurator) setRules(ingress *v1beta1.Ingress, descriptor *types.Descriptor, service *v1.Service) error {
	ingress.Spec.Rules = []v1beta1.IngressRule{
		{
			Host: descriptor.Frontend,
			IngressRuleValue: v1beta1.IngressRuleValue{
				HTTP: &v1beta1.HTTPIngressRuleValue{
					Paths: []v1beta1.HTTPIngressPath{
						{
							Backend: v1beta1.IngressBackend{
								ServiceName: service.Name,
								ServicePort: findIngressPort(service.Spec.Ports).TargetPort,
							},
						},
					},
				},
			},
		},
	}
	return nil
}

func (ic *IngressConfigurator) setTlsConfig(ingress *v1beta1.Ingress, descriptor *types.Descriptor, logger logger.Logger) error {

	// TODO check descriptor for tls secret name, and fall back to domain if not set
	secretName, err := extractDomain(descriptor.Frontend)
	if err != nil {
		return errors.New(fmt.Sprintf("Couldn't parse frontend for 2nd level domain, can not create Ingress!\n%v", err.Error()))
	}

	logger.Printf("Searching for TLS secret %v", secretName)
	_, err = ic.k8sClient.GetSecret(descriptor.Namespace, secretName)
	if statusError, isStatus := err.(*k8sErrors.StatusError); isStatus && statusError.ErrStatus.Reason == meta.StatusReasonNotFound {
		return errors.New(fmt.Sprintf("  Didn't find secret %v, can not create Ingress!", secretName))
	} else if err != nil {
		return err
	}

	ingress.Spec.TLS = []v1beta1.IngressTLS{
		{
			Hosts:      []string{descriptor.Frontend},
			SecretName: secretName,
		},
	}
	return nil
}

func (ic *IngressConfigurator) DeleteProxy(deployment *types.Deployment, logger logger.Logger) error {
	return ic.k8sClient.DeleteIngress(deployment.Descriptor.Namespace, deployment.Descriptor.AppName)
}

func findIngressPort(ports []v1.ServicePort) v1.ServicePort {
	if len(ports) > 1 {
		for _, port := range ports {
			if port.Name != "healthcheck" {
				return port
			}
		}
	}

	return ports[0]
}

func extractDomain(host string) (string, error) {
	return publicsuffix.EffectiveTLDPlusOne(host)
}

func removePrefixSpacesInHeaderValues(headers []types.HttpHeader) {
	// HAProxy needed prefixed spaces, onfortunately we stored them
	// For Ingresses we need to remove them again...
	for i, header := range headers {
		value := header.Value
		// prefix spaces with \, but only if that wasn't done before
		value = strings.Replace(value, " ", "\\ ", -1)
		value = strings.Replace(value, "\\\\ ", "\\ ", -1)
		headers[i].Value = value
	}
}
