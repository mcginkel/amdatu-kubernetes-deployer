Introduction
===
Amdatu Kubernetes Deployer is a component to orchestrate Kubernetes deployments with the following features:

* Blue-green deployment
* External load balancer configuration
* Health checks during deployment
* Deployment history
* Injecting environment variables into pods

The component is built on top of the Kubernetes API.
It provides both a REST and WebSocket API.
The Amdatu Kubernetes Deployer is typically used together with the Amdatu Kubernetes Deploymentctl UI, or invoked as part of a build pipeline.

Amdatu Kubernetes Deployer is used in several production environments, and is actively maintained.

![overview](overview.jpg)

Related components
===
There are several related Amdatu components that work very well together, but are loosely coupled.

* Amdatu Kubernetes Deploymentctl is a UI for the Amdatu Kubernetes Deployer
* Amdatu Ha-proxy confd is a configuration template to use Ha-proxy together with Kubernetes and the Amdatu Kubernetes Deployer.

Getting started
===

Run as a Docker container:

```
docker run amdatu/amdatu-kubernetes-deployer:prod -kubernetes http://[kubernetes-api-server]:8080 -etcd http://[etcd-server]:2379
docker run amdatu/haproxy-confd:prod ...
```

Load balancing
===
To make applications available to the internet, a load balancer is used in front of Kubernetes.
Although Kubernetes has some primitives built in for load balancing, it is currently not very useful if you're not running on GCE.
Amdatu Kubernetes Deployer is designed to work with different load balancers.
Amdatu Ha-proxy confd provides out-of-the-box support for Ha-proxy, and can be used as an example for integrating other load balancers.
Amdatu Kubernetes Deployer uses load balancer configuration in etcd.
Based on this configuration in etcd a tool like [confd](https://github.com/kelseyhightower/confd) can be used to generate configuration for a load balancer.

The configuration schema is defined [here](proxy-config.md).

Deployment descriptor
===
The following JSON represents a deployment descriptor to start a deployment.

```
{
  "deploymentType": "blue-green",
  "namespace": "default",
  "useHealthCheck": true,
  "newVersion": "#",
  "appName": "nginx-demo",
  "replicas": 2,
  "frontend": "my-example.amdatu.org",
  "podspec": {
    #Kubernetes pod spec
  }
}
```

The most important part of the deployment descriptor is the `podspec`, which is defined by the [Kubernetes API](http://kubernetes.io/docs/api-reference/v1/definitions/#_v1_podspec).
The remaining fields are described below.

|Field   |Description   |
|---|---|
|deploymentType   |Type of roll-out stategy. Currently only `blue-green` is supported|
|namespace   |The Kubernetes namespace to deploy to   |
|useHealthCheck   |Use health checks during deployment. Deployment only succeeds if health checks are ok. Learn more about health checks [here](#healthchecks).   |
|newVersion   | Version of the deployment. Use `#` for automatic version increments  |
|appName   | Name of the deployed component. Must follow Kubernetes [naming rules](https://github.com/kubernetes/kubernetes/blob/release-1.2/docs/design/identifiers.md) |
|replicas   | Number of replicas for the Replication Controller |
|frontend   | Name in the frontend (load balancer) configuration to use. Optional. |

Health checks
===
Health checks should be implemented as part of the application.
When health checks are disabled, the Amdatu Kubernetes Deployer expects them on `/health` in the first container in the pod.
When multiple ports are configured in the container, the health check port should be named `healthcheck`.
If no ports are configured in on container, port 9999 is assumed.

The `/health` endpoint should return JSON in the following format.

```
{"healthy" : true}
```

Additional properties are allowed, but ignored by the Amdatu Kubernetes Deployer.

Deployment naming and versioning
===
For each deployment the following resources are created in Kubernetes.

|Resource | Name | Description   |
|---|---|---|
|service   |appName| Service that is *not* versioned. This service can be used from other components, because it stays around between deployments. |
|service   |appName-newVersion| Service that is versioned. This service is used by the load balancer. Each deployment will create a new service |
|replication controller   |appName-newVersion| Replication controller for the specific version of the deployment. Each deployment will create a new replication controller|

Deployment history
===
A deployment history is kept in etcd.

Keys are in the following format:

```
/deployment/[namespace]/[UID]/[data]
```

The value is the complete deployment descriptor.

Environment variables
===
It's possible to inject extra environment variables into pods.
This is useful if pods need to discover certain infrastructure services outside Kubernetes, without the need to put them in the deployment descriptor.
To inject an environment variable, a key has to be created in etcd:

```
/deployer/environment/[mykey]
```

The key will be the environment variable name, the value of the etcd key will be the value of the environment variable.

Authentication and authorization
===
For authentication against the Kubernetes API, basic authentication is supported.
Credentials need to be provided as program arguments.
There is also preliminary support for namespace authorization.
A HTTP server providing the following endpoints can be run to take care of the actual authentication and authorization:

* POST /auth/login
* GET /rtiauth/namespaces

To learn about the data format used, take a look at [auth/http_authenticator_test.go](auth/http_authenticator_test.go).
**Warning**, the authorization mechanism is likely to be changed soon.
CoreOS dex is a likely candidate for a future implementation.

Getting involved
===

[Bug reports and feature requests](https://amdatu.atlassian.net/projects/AKD) and of course pull requests are greatly appreciated!
The project is built on [Bamboo](https://amdatu.atlassian.net/builds/browse/AKD-MAIN/latest).
The build produces cross platform binaries and pushed a Docker image.

Versioning
===

The Docker images are tagged using a alpha/beta/prod schema.
The continuous build pushes `amdatu/amdatu-kubernetes-deployer:alpha`.
When a version is stable, we promote it to `beta`.
The image is not rebuild, but a tag is set in git specifying the version number (e.g. 1.x.x).
For Docker we set multiple tags:

* beta
* 1.x.x

This way you can both reference the latest beta version, or a specific tagged version.
From beta we promote images to `production`.
The image is not rebuild, but new tags are added:

* prod
* production
* latest

This way you can reference the latest stable production version.