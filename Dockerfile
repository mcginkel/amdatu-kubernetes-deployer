FROM busybox
ADD deployer /deployer
WORKDIR /
ENTRYPOINT ["/deployer"]