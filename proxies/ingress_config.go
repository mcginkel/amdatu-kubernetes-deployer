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

	"errors"
	"fmt"

	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/k8s"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/logger"
	"bitbucket.org/amdatulabs/amdatu-kubernetes-deployer/types"
	"golang.org/x/net/publicsuffix"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/apis/extensions/v1beta1"
)

type IngressConfigurator struct {
	k8sClient *k8s.K8sClient
	nginx     *NginxStatus
}

func NewIngressConfigurator(k8sClient *k8s.K8sClient, proxyReload int, healthTimeout int) *IngressConfigurator {
	return &IngressConfigurator{
		k8sClient: k8sClient,
		nginx:     NewNginxStatus(k8sClient, proxyReload, healthTimeout),
	}
}

func (ic *IngressConfigurator) CreateOrUpdateProxy(deployment *types.Deployment,
	service *v1.Service, logger logger.Logger) error {

	descriptor := deployment.Descriptor

	logger.Printf("Getting Ingress for %v", descriptor.AppName)
	var oldIngress v1beta1.Ingress
	ingress, err := ic.k8sClient.GetIngress(descriptor.Namespace, descriptor.AppName)
	if err == nil {

		logger.Println("  found existing Ingress, updating")
		oldIngress = *ingress // copy values, not pointer!

		if err := ic.configure(ingress, descriptor, service, logger); err != nil {
			return err
		}

		if ingress, err = ic.k8sClient.UpdateIngress(descriptor.Namespace, ingress); err != nil {
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

		if ingress, err = ic.k8sClient.CreateIngress(descriptor.Namespace, ingress); err != nil {
			return err
		}

	} else {
		return err
	}

	err = ic.nginx.WaitForProxy(deployment, findIngressPort(service.Spec.Ports).TargetPort.IntVal, logger)
	if err != nil {
		if oldIngress.Name != "" {
			logger.Println("Resetting Ingress")
			ingress.Spec = oldIngress.Spec
			if _, err2 := ic.k8sClient.UpdateIngress(descriptor.Namespace, ingress); err2 != nil {
				logger.Printf("Error resetting Ingress: %v", err2.Error())
			}
		}
		return err
	}

	// create www redirect ingress
	if descriptor.RedirectWww {

		logger.Printf("Getting persistent service for wwww redirect of %v", descriptor.AppName)
		wwwService, err := ic.getPersistentService(deployment)
		if err != nil {
			return err
		}

		logger.Printf("Getting Ingress for wwww redirect of %v", descriptor.AppName)
		wwwIngressName := getWwwRedirectName(deployment.Descriptor)
		rewriteSnippetKey := "ingress.kubernetes.io/configuration-snippet"
		rewriteSnippetValue := "rewrite ^/(.*)$ https://" + descriptor.Frontend + "/$1 permanent;"

		ingress, err := ic.k8sClient.GetIngress(descriptor.Namespace, wwwIngressName)
		if err == nil {

			logger.Println("  found existing www redirect Ingress, updating")

			ingress.Annotations[rewriteSnippetKey] = rewriteSnippetValue
			ic.setRules(ingress, descriptor, wwwService, true)
			ic.setTlsConfig(ingress, descriptor, true, logger)

			if _, err := ic.k8sClient.UpdateIngress(descriptor.Namespace, ingress); err != nil {
				return err
			}

		} else if statusError, isStatus := err.(*k8sErrors.StatusError); isStatus && statusError.Status().Reason == meta.StatusReasonNotFound {

			logger.Println("  no www redirect Ingress found, creating new one")

			ingress = &v1beta1.Ingress{}

			ingress.Namespace = descriptor.Namespace
			ingress.Name = wwwIngressName

			annotations := make(map[string]string)
			annotations["kubernetes.io/ingress.class"] = "nginx"
			annotations[rewriteSnippetKey] = rewriteSnippetValue
			ingress.Annotations = annotations

			ic.setRules(ingress, descriptor, wwwService, true)
			ic.setTlsConfig(ingress, descriptor, true, logger)

			if _, err := ic.k8sClient.CreateIngress(descriptor.Namespace, ingress); err != nil {
				return err
			}

		} else {
			return err
		}
	} else {
		// check if old www redirect exists
		wwwIngressName := getWwwRedirectName(deployment.Descriptor)
		_, err = ic.k8sClient.GetIngress(deployment.Descriptor.Namespace, wwwIngressName)
		if err == nil {
			err = ic.k8sClient.DeleteIngress(deployment.Descriptor.Namespace, wwwIngressName)
			if err != nil {
				return err
			}
		} else if statusError, isStatus := err.(*k8sErrors.StatusError); isStatus && statusError.Status().Reason == meta.StatusReasonNotFound {
			// everything is fine
		} else {
			return err
		}
	}

	return nil
}

func (ic *IngressConfigurator) configure(ingress *v1beta1.Ingress, descriptor *types.Descriptor, service *v1.Service, logger logger.Logger) error {
	if err := ic.setTlsConfig(ingress, descriptor, false, logger); err != nil {
		return err
	}
	ic.setRules(ingress, descriptor, service, false)
	ic.setAffiity(ingress, descriptor)

	// get configuration snippets
	snippetKey := "ingress.kubernetes.io/configuration-snippet"
	snippetValue := ""
	snippetValue = ic.addGzipSnippet(snippetValue, descriptor)
	snippetValue = ic.addAdditionalHeadersSnippet(snippetValue, descriptor)
	if len(snippetValue) > 0 {
		ingress.Annotations[snippetKey] = snippetValue
	} else {
		delete(ingress.Annotations, snippetKey)
	}

	return nil
}

func (ic *IngressConfigurator) setRules(ingress *v1beta1.Ingress, descriptor *types.Descriptor, service *v1.Service, withWww bool) {
	host := descriptor.Frontend
	if withWww {
		host = "www." + host
	}
	ingress.Spec.Rules = []v1beta1.IngressRule{
		{
			Host: host,
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
}

func (ic *IngressConfigurator) setTlsConfig(ingress *v1beta1.Ingress, descriptor *types.Descriptor, withWww bool, logger logger.Logger) error {

	secretName, err := ic.findTlsSecret(descriptor)
	if err != nil {
		return errors.New(fmt.Sprintf("Couldn't find TLS secret, can not create Ingress!\n%v", err.Error()))
	}

	host := descriptor.Frontend
	if withWww {
		host = "www." + host
	}

	ingress.Spec.TLS = []v1beta1.IngressTLS{
		{
			Hosts:      []string{host},
			SecretName: secretName,
		},
	}
	return nil
}

func (ic *IngressConfigurator) findTlsSecret(descriptor *types.Descriptor) (string, error) {

	// first try secret specified in descriptor
	secretName := descriptor.TlsSecretName
	if len(secretName) > 0 && ic.secretExists(descriptor.Namespace, secretName) {
		return secretName, nil
	}

	// then try full frontend name
	secretName = descriptor.Frontend
	if ic.secretExists(descriptor.Namespace, secretName) {
		return secretName, nil
	}

	// try 2nd level domain
	secretName, err := extractDomain(descriptor.Frontend)
	if err != nil {
		return "", err
	}
	if ic.secretExists(descriptor.Namespace, secretName) {
		return secretName, nil
	}

	return "", errors.New("No secret found")
}

func (ic *IngressConfigurator) secretExists(namespace, secretName string) bool {
	_, err := ic.k8sClient.GetSecret(namespace, secretName)
	return err == nil
}

func (ic *IngressConfigurator) setAffiity(ingress *v1beta1.Ingress, descriptor *types.Descriptor) {
	affinity := "ingress.kubernetes.io/affinity"
	if descriptor.UseStickySessions {
		ingress.Annotations[affinity] = "cookie"
	} else {
		delete(ingress.Annotations, affinity)
	}
}

func (ic *IngressConfigurator) addGzipSnippet(snippet string, descriptor *types.Descriptor) string {
	if descriptor.UseCompression {
		snippet += "gzip on;\n" +
			"gzip_comp_level 5;\n" +
			"gzip_http_version 1.1;\n" +
			"gzip_min_length 256;\n" +
			"gzip_types application/atom+xml application/javascript application/json application/ld+json application/manifest+json application/rss+xml application/vnd.geo+json application/vnd.ms-fontobject application/x-font-ttf application/x-javascript application/x-web-app-manifest+json application/xhtml+xml application/xml font/opentype image/bmp image/svg+xml image/x-icon text/cache-manifest text/css text/javascript text/plain text/vcard text/vnd.rim.location.xloc text/vtt text/x-component text/x-cross-domain-policy;\n" +
			"gzip_proxied any;\n"
	}
	return snippet
}

func (ic *IngressConfigurator) addAdditionalHeadersSnippet(snippet string, descriptor *types.Descriptor) string {
	if descriptor.AdditionHttpHeaders != nil && len(descriptor.AdditionHttpHeaders) > 0 {

		// needed because in an earlier version we escaped spaces in headers for HAProxy and saved them back...
		removePrefixSpacesInHeaderValues(descriptor.AdditionHttpHeaders)

		snippet += "more_set_headers "
		for _, header := range descriptor.AdditionHttpHeaders {
			snippet += "'" + header.Header + ": "
			snippet += header.Value + "' "
		}
		snippet += ";"
	}
	return snippet
}

func (ic *IngressConfigurator) DeleteProxy(deployment *types.Deployment, logger logger.Logger) error {

	// ignore not found errors...

	err1 := ic.k8sClient.DeleteIngress(deployment.Descriptor.Namespace, deployment.Descriptor.AppName)
	if statusError, isStatus := err1.(*k8sErrors.StatusError); isStatus && statusError.Status().Reason == meta.StatusReasonNotFound {
		err1 = nil
	}

	var err2 error
	wwwIngressName := getWwwRedirectName(deployment.Descriptor)
	_, err2 = ic.k8sClient.GetIngress(deployment.Descriptor.Namespace, wwwIngressName)
	if err2 == nil {
		err2 = ic.k8sClient.DeleteIngress(deployment.Descriptor.Namespace, wwwIngressName)
	} else if statusError, isStatus := err2.(*k8sErrors.StatusError); isStatus && statusError.Status().Reason == meta.StatusReasonNotFound {
		err2 = nil
	}

	msg := ""
	if err1 != nil {
		msg = err1.Error()
	}
	if err2 != nil {
		msg += ", " + err2.Error()
	}
	if len(msg) > 0 {
		return errors.New(msg)
	}
	return nil
}

func (ic *IngressConfigurator) getPersistentService(deployment *types.Deployment) (*v1.Service, error) {
	return ic.k8sClient.GetService(deployment.Descriptor.Namespace, deployment.Descriptor.AppName)

}

func getWwwRedirectName(descriptor *types.Descriptor) string {
	return descriptor.AppName + "-www-redirect"
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
	// HAProxy needed prefixed spaces, unfortunately we stored them
	// For Ingresses we need to remove them again...
	for i, header := range headers {
		value := header.Value
		value = strings.Replace(value, "\\ ", " ", -1)
		headers[i].Value = value
	}
}
