package main

import (
	"os"
	"testing"
)

func TestDockerDaemonAPIVersion(t *testing.T) {
	version, err := dockerDaemonAPIVersion()

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if version == "" {
		t.Errorf("got empty version")
	}
}

func TestDockerDaemonAPIVersionNoDockerAvailable(t *testing.T) {
	oldPath := os.Getenv("PATH")
	os.Setenv("PATH", "/")
	defer os.Setenv("PATH", oldPath)

	version, err := dockerDaemonAPIVersion()

	if err == nil {
		t.Errorf("expected error: %v", err)
	}

	if version != "" {
		t.Errorf("didn't get an empty version")
	}
}
