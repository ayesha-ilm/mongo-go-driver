// Copyright (C) MongoDB, Inc. 2026-present.
//
// Licensed under the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License. You may obtain
// a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

package prose

import (
	"context"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/joho/godotenv"
	"github.com/moby/moby/api/types/container"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

var (
	loadSecretsFlag = flag.Bool("load-secrets", false, "Use AWS SSO login to load secrets into the repository root")
	cseFlag         = flag.Bool("cse", false, "Setup required for running CSE tests")
)

func secretsRequested() bool {
	return *loadSecretsFlag || *cseFlag
}

func TestMain(m *testing.M) {
	flag.Parse()

	// Ryuk removes containers when the test process exits, which would defeat
	// reusing the CSE container across runs.
	os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")

	if secretsRequested() {
		path, err := exportSecrets()
		if err != nil {
			log.Panicf("error loading secrets: %v", err)
		}
		if err := godotenv.Overload(path); err != nil {
			log.Panicf("error loading secrets: %v", err)
		}
	}

	os.Exit(m.Run())
}

// =============================================================================
// Test Runner Helpers
// =============================================================================

const (
	dockerRepo       = "10gen/go-driver-tools"
	dockerfileName   = "aws-sso-login.Dockerfile"
	entrypointName   = "aws-sso-login.sh"
	secretsFileName  = "secrets-export.sh"
	containerOutDir  = "/out"
	loginTimeout     = 10 * time.Minute
	secretsMaxAge    = 50 * time.Minute
	secretsDirPrefix = "mongo-go-driver-prose"
)

func keepExports(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", secretsFileName, err)
	}

	var exports strings.Builder
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "export ") {
			exports.WriteString(line + "\n")
		}
	}

	return os.WriteFile(path, []byte(exports.String()), 0o600)
}

// exportSecrets returns the path to secrets-export.sh, running the AWS SSO
// login container to create it if it is missing or stale. The secrets-export.sh
// file lives in the Go Driver repository root.
func exportSecrets() (string, error) {
	repoRoot, err := filepath.Abs("../../../")
	if err != nil {
		return "", fmt.Errorf("failed to resolve relative path: %w", err)
	}
	secretsPath := filepath.Join(repoRoot, secretsFileName)

	// The file holds temporary AWS, Azure and GCP tokens that expire about an
	// hour after the login, but it does not record when. Reuse it only while
	// it is younger than secretsMaxAge; after that, log in again.
	if info, err := os.Stat(secretsPath); err == nil && time.Since(info.ModTime()) < secretsMaxAge {
		return secretsPath, nil
	}

	buildDir, err := os.MkdirTemp("", secretsDirPrefix+"-build")
	if err != nil {
		return "", fmt.Errorf("failed to create build context: %w", err)
	}
	defer os.RemoveAll(buildDir)

	if err := downloadDockerfile(buildDir); err != nil {
		return "", err
	}

	if err := runLogin(buildDir, repoRoot); err != nil {
		return "", err
	}

	if _, err := os.Stat(secretsPath); err != nil {
		return "", fmt.Errorf("AWS SSO login container did not write %s: %w", secretsFileName, err)
	}

	if err := keepExports(secretsPath); err != nil {
		return "", err
	}

	return secretsPath, nil
}

// downloads the Dockerfile and entrypoint from the private repo.
func downloadDockerfile(dir string) error {
	client, err := api.DefaultRESTClient()
	if err != nil {
		return fmt.Errorf("failed to create GitHub client (is `gh auth login` set up?): %w", err)
	}

	for _, name := range []string{dockerfileName, entrypointName} {
		var response struct {
			Content string `json:"content"`
		}

		if err := client.Get("repos/"+dockerRepo+"/contents/docker/"+name, &response); err != nil {
			return fmt.Errorf("failed to fetch %s from %s: %w", name, dockerRepo, err)
		}

		content, err := base64.StdEncoding.DecodeString(response.Content)
		if err != nil {
			return fmt.Errorf("failed to decode %s: %w", name, err)
		}

		if err := os.WriteFile(filepath.Join(dir, name), content, 0o755); err != nil {
			return fmt.Errorf("failed to write %s: %w", name, err)
		}
	}

	return nil
}

// runLogin builds the image in buildDir and runs it with secretsDir mounted
// at the container's output directory. The login is interactive: container
// output is streamed to stderr so the verification URL and code are visible.
func runLogin(buildDir, secretsDir string) error {
	ctx, cancel := context.WithTimeout(context.Background(), loginTimeout)
	defer cancel()

	req := testcontainers.ContainerRequest{
		FromDockerfile: testcontainers.FromDockerfile{
			Context:    buildDir,
			Dockerfile: dockerfileName,
		},
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.Binds = append(hc.Binds, secretsDir+":"+containerOutDir)
		},
		LogConsumerCfg: &testcontainers.LogConsumerConfig{
			Consumers: []testcontainers.LogConsumer{stderrLogConsumer{}},
		},
		// The entrypoint exits once the secrets are written.
		WaitingFor: wait.ForExit().WithExitTimeout(loginTimeout),
	}

	ctr, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	defer func() {
		if ctr != nil {
			_ = ctr.Terminate(context.Background())
		}
	}()
	if err != nil {
		return fmt.Errorf("failed to run AWS SSO login container: %w", err)
	}

	state, err := ctr.State(ctx)
	if err != nil {
		return fmt.Errorf("failed to get AWS SSO login container state: %w", err)
	}
	if state.ExitCode != 0 {
		return fmt.Errorf("AWS SSO login container exited with code %d", state.ExitCode)
	}

	return nil
}

type stderrLogConsumer struct{}

func (stderrLogConsumer) Accept(log testcontainers.Log) {
	fmt.Fprintln(os.Stderr, "aws-sso-login:", strings.TrimRight(string(log.Content), "\n"))
}

const (
	CSEDockerfile    = "internal/test/docker/cse.Dockerfile"
	cseContainerName = "mongo-go-driver-cse"
)

// StartCSE starts the CSE container, building the image from the repository
// root only if no container named cseContainerName exists yet. The container is
// reused across runs and keeps running so that multiple commands can be
// executed against it with Exec. The repository root is mounted into the
// container, so driver changes are picked up without a rebuild.
func StartCSE(t *testing.T) testcontainers.Container {
	t.Helper()

	rootDir, err := findRepoRoot()
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
		Name:       cseContainerName,
		WorkingDir: "/mongo-go-driver",
		HostConfigModifier: func(hc *container.HostConfig) {
			hc.Binds = append(hc.Binds, rootDir+":/mongo-go-driver")
		},
	}

	c, err := testcontainers.GenericContainer(context.Background(), testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
		Reuse:            true,
	})
	if err != nil {
		t.Fatalf("failed to start CSE container: %v", err)
	}

	return c
}

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
	for len(s) > 0 && s[0] < 0x20 {
		s = s[1:]
	}
	return exit, s, nil
}

// findRepoRoot walks up from wd until it finds the CSE Dockerfile.
func findRepoRoot() (string, error) {
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
