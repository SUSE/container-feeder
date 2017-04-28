package main

import (
	"os/exec"
	"testing"
)

func TestNewFeeder(t *testing.T) {
	feeder, err := NewFeeder()

	if err != nil {
		t.Errorf("error while starting feeder: %v", err)
	}

	test_dir := "./tests/images"
	importResp, err := feeder.Import(test_dir)
	if err != nil {
		t.Errorf("error while importing test image: %v", err)
	}

	if len(importResp.SuccessfulImports) != 1 {
		t.Errorf("error with importing test image")
	}

	//Test cleanup
	//Since removing images function wasn't implemented
	//We have to drop test image manually
	cmd := exec.Command("docker", "rmi", "containter-feeder/test-image:10.1")
	err = cmd.Run()
	if err != nil {
		t.Errorf("error with cleaning up test setup: %v", err)
	}
}
