#!/usr/bin/env bash
# run-cse-container-test
# Build the CSE Docker image and run the CSE container tests against it.
set -eu
set +x

echo "Running internal/test/container"
pushd internal/test/container
go test -timeout 30m -v ./... >>../../../test.suite
popd
