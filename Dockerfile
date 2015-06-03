FROM busybox
ADD main /main
WORKDIR /
ENTRYPOINT ["/main"]