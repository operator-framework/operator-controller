# Note: This dockerfile does not build the binaries 
# required and is intended to be built only with the
# 'make build' or 'make release' targets.
FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY manager manager

EXPOSE 8080

USER 65532:65532

ENTRYPOINT ["/manager"]