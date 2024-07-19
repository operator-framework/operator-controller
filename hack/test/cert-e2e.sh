#!/bin/bash

set -e

# patch the certificates to renew every minute
# (if set to 2 minutes, then the big timeout wait is about 5 minutes)
kubectl patch certificate.cert-manager.io -n cert-manager olmv1-ca --type='merge' -p '{"spec":{"duration":"1h", "renewBefore":"59m"}}'
kubectl patch certificate.cert-manager.io -n olmv1-system olmv1-cert --type='merge' -p '{"spec":{"duration":"1h", "renewBefore":"59m"}}'

# delete the old secrets, so new secrets are generated
kubectl delete secret -n cert-manager olmv1-ca
kubectl delete secret -n olmv1-system olmv1-cert

# and then search through the previous logs
function check_logs() {
    kubectl logs -c manager -n olmv1-system -l control-plane=controller-manager | grep "updating certificate pool" >& /dev/null
}

WAIT_TIME=0
MAX_TIME=300
until (( WAIT_TIME == MAX_TIME )) || check_logs; do
    echo -n "."
    sleep "$(( WAIT_TIME += 10 ))"
done
(( WAIT_TIME < MAX_TIME ))
echo "#"
