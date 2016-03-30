FROM busybox
ADD deployer_linux_amd64 /deployer
WORKDIR /
ENTRYPOINT ["/deployer"]