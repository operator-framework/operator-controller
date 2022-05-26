FROM gcr.io/distroless/static:debug
WORKDIR /

COPY manager manager
EXPOSE 8080
