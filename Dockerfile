FROM alpine:3.5

COPY ./amdatu-kubernetes-deployer /go/bin/amdatu-kubernetes-deployer

ENV PATH="/go/bin:$PATH"
WORKDIR /go/bin

ENTRYPOINT ["amdatu-kubernetes-deployer"]