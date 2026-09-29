#!/usr/bin/env bash
# run-cse-container-test
set -eu
set +x

echo "Running internal/test/container"
pushd internal/test/container
go test -timeout 30m -v ./... >>../../../test.suite
popd
