/proxy/configuration
===
```
{"DockerRegistry": "<boolean>", "HTTPSMode": "<string>", "OffloadSSL": "<boolean>"}
```
**DockerRegistry**: if true, listen on port 5000 and allow docker registry backend servers to be served. If false: don't listen on port 5000

**OffloadSSL**:
true = also listen on 443 and act as a real HTTPS server
false = only listen on port 80

**HTTPSMode**:
only = redirect all HTTP traffic on port 80 to HTTPS
mixed = allow both HTTP and HTTPS traffic
disabled = enable on HTTP traffic and disable HTTPS completely

/proxy/backends/[deployment]/[ip-address]
===

```
{"IPAddress": "<ip-address>", "Port": "<ip-port>", "CompressionEnabled": <boolean>}
```

Every deployment hosted by Kubernetes offers a ip-address + port to offer a service, which needs to be proxied for access by the rest of the world
CompressionEnabled is an optional value as it was later introduced. The default value if this property is not set should be FALSE

Read: to get insights into the available backends
Delete: handy for debugging: state should always be up2date, but one might need to delete a backend that doesn't exist anymore and wasn't removed properly

/proxy/frontends/[hostname]
===

```
{"Type": "<deploymenttype>", "BackendId": "<backend>"}
```

Frontend points to the backend to use: this allows us the create multiple backends for one host, which we need during blue-green deployments
deploymenttype values:
* http
* docker-registry
