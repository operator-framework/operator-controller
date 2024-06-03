# Note: This dockerfile does not build the binaries 
# required and is intended to be built only with the
# 'make build' or 'make release' targets.
# Stage 1:
FROM gcr.io/distroless/static:debug-nonroot AS builder

# Stage 2: 
FROM gcr.io/distroless/static:nonroot

# Grab the cp binary so we can cp the unpack
# binary to a shared volume in the bundle image (rukpak library needs it)
COPY --from=builder /busybox/cp /cp

WORKDIR /

COPY manager manager

EXPOSE 8080

USER 65532:65532
