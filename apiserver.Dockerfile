# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details

#Note: PoC is running as root but we can run this as nonroot by making the apiserver listen on 8443 internally and then map 443 to 8443 in the Service
# eg this is what the openshift-apiserver does
# more info: https://coreos.slack.com/archives/G3T7N42NP/p1667580247206729?thread_ts=1667577965.339339&cid=G3T7N42NP
    # ports:
    # - name: https
    #   port: 443
    #   protocol: TCP
    #   targetPort: 8443
FROM gcr.io/distroless/static:latest    
WORKDIR /
COPY apiserver .

ENTRYPOINT ["/apiserver"]