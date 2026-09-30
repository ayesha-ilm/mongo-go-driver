// Copyright (C) MongoDB, Inc. 2026-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package prose

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStartCSE(t *testing.T) {
	c := StartCSE(t)

	exit, out, err := Exec(context.Background(), c, "go test -tags cse ./x/mongo/driver/mongocrypt")
	require.NoError(t, err)
	require.Equal(t, 0, exit, "go test failed: %s", out)
}
