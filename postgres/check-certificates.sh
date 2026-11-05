#!/bin/sh
# Public certificate inventory only. Private keys must never enter Git.
set -eu
cd "$(dirname "$0")"
for certificate in tls/ca.crt tls/server.crt; do
  openssl x509 -in "$certificate" -noout -checkend 7776000
done
openssl verify -CAfile tls/ca.crt -verify_hostname perry-infra_postgres tls/server.crt
