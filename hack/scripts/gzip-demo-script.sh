#!/usr/bin/env bash

trap "trap - SIGTERM && kill -- -$$" SIGINT SIGTERM EXIT
# Welcome to the catalogd demo
make run

# create a clustercatalog
kubectl apply -f $HOME/devel/tmp/operatorhubio-clustercatalog.yaml
# shows catalog
kubectl get clustercatalog -A 
# waiting for clustercatalog to report ready status
time kubectl wait --for=condition=Unpacked clustercatalog/operatorhubio --timeout=1m

# port forward the catalogd-catalogserver service to interact with the HTTP server serving catalog contents
(kubectl -n olmv1-system port-forward svc/catalogd-catalogserver 8080:443)&
sleep 5

# retrieve catalog as plaintext JSONlines
curl -k -vvv https://localhost:8080/catalogs/operatorhubio/all.json --output /tmp/cat-content.json

# advertise handling of compressed content
curl -vvv -k https://localhost:8080/catalogs/operatorhubio/all.json -H 'Accept-Encoding: gzip' --output /tmp/cat-content.gz

# let curl handle the compress/decompress for us
curl -vvv --compressed -k https://localhost:8080/catalogs/operatorhubio/all.json --output /tmp/cat-content-decompressed.txt

# show that there's no content change with changed format
diff /tmp/cat-content.json /tmp/cat-content-decompressed.txt

