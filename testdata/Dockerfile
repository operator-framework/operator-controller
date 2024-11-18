FROM gcr.io/distroless/static:nonroot

WORKDIR /

COPY registry/bin/registry registry
COPY push/bin/push push

COPY images images

EXPOSE 5000

USER 65532:65532
