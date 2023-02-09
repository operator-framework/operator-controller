# Build the manager binary
FROM golang:1.19 as builder

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/apiserver/main.go main.go
COPY pkg/apis/ pkg/apis/


# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o apiserver main.go

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
COPY --from=builder /workspace/apiserver .


ENTRYPOINT ["/apiserver"]