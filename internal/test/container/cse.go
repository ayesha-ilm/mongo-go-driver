// Copyright (C) MongoDB, Inc. 2026-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

// Package container provides testcontainers helpers for running driver tests
// inside Docker images.
package container

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/testcontainers/testcontainers-go"
)

// CSEDockerfile is the path of the CSE Dockerfile relative to the repository
// root.
const CSEDockerfile = "internal/test/docker/cse.Dockerfile"

// StartCSE builds the CSE image from the repository root and starts a container
// that stays running for the lifetime of the test so that multiple commands can
// be executed against it with Exec. The container is terminated on test
// cleanup.
func StartCSE(t *testing.T) testcontainers.Container {
	t.Helper()

	rootDir, err := repoRoot()
	if err != nil {
		t.Fatalf("failed to find repository root: %v", err)
	}

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:       rootDir,
			Dockerfile:    CSEDockerfile,
			PrintBuildLog: true,
		},
		// Block on "tail -f /dev/null" so the container stays alive and ready
		// for exec calls, rather than immediately exiting.
		Entrypoint: []string{"tail", "-f", "/dev/null"},
		WorkingDir: "/mongo-go-driver",
	}

	c, err := testcontainers.GenericContainer(context.Background(), testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		t.Fatalf("failed to start CSE container: %v", err)
	}

	t.Cleanup(func() {
		if err := c.Terminate(context.Background()); err != nil {
			t.Errorf("failed to terminate CSE container: %v", err)
		}
	})

	return c
}

// Exec runs cmd with "bash -c" in the container and returns the exit code and
// combined output. A non-login shell is used so the image's ENV settings (PATH,
// PKG_CONFIG, LD_LIBRARY_PATH) are not reset by /etc/profile.
func Exec(ctx context.Context, c testcontainers.Container, cmd string) (int, string, error) {
	exit, out, err := c.Exec(ctx, []string{"bash", "-c", cmd + " 2>&1"})
	if err != nil {
		return 0, "", fmt.Errorf("failed to exec %q: %w", cmd, err)
	}

	b, err := io.ReadAll(out)
	if err != nil {
		return 0, "", fmt.Errorf("failed to read output of %q: %w", cmd, err)
	}

	s := string(b)
	// Strip leading non-printable bytes (some Docker/TTY combos emit these).
	for len(s) > 0 && s[0] < 0x20 {
		s = s[1:]
	}
	return exit, s, nil
}

// repoRoot walks up from the working directory until it finds the directory
// containing the CSE Dockerfile.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, CSEDockerfile)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("%s not found in any parent directory", CSEDockerfile)
		}
		dir = parent
	}
}
