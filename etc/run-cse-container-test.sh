#!/usr/bin/env bash
# run-cse-container-test
set -eu
set +x

echo "Running internal/test/prose CSE container test"
pushd internal/test/prose
go test -timeout 30m -v -run TestStartCSE ./... >>../../../test.suite
popd
