package main

import (
	"context"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"
)

// Figure the API version supported by the server
// by shelling out.
func dockerDaemonAPIVersion() string {
	out, err := exec.Command(
		"docker",
		"version",
		"--format",
		"{{.Server.APIVersion}}").Output()
	if err != nil {
		panic(err)
	}
	api := strings.Trim(string(out[:]), "\n")
	return api
}

// Connects to the local daemon using the right version of the API
func connectToDaemon() (*client.Client, error) {
	// Set the exact version of the API in use, otherwise the library will
	// try to use the latest one, which might be too newer compared to the
	// one supported by the docker daemon
	if err := os.Setenv("DOCKER_API_VERSION", dockerDaemonAPIVersion()); err != nil {
		return nil, err
	}

	return client.NewEnvClient()
}

// Returns images available on the docker host
// The images are stored inside of a list of strings where
// each string is following this convention: "<repo>:<tag>"
func existingImages(cli *client.Client) ([]string, error) {
	tags := []string{}

	images, err := cli.ImageList(context.Background(), types.ImageListOptions{})
	if err != nil {
		return tags, err
	}

	for _, image := range images {
		for _, tag := range image.RepoTags {
			tags = append(tags, tag)
		}
	}

	return tags, nil
}

// Loads the specified image into docker
// Returns the message produced by the docker daemon
func loadDockerImage(cli *client.Client, pathToImage string) (string, error) {
	image, err := os.Open(pathToImage)
	if err != nil {
		return "", err
	}
	defer image.Close()

	ret, err := cli.ImageLoad(context.Background(), image, true)
	if err != nil {
		return "", err
	}

	defer ret.Body.Close()

	b, err := ioutil.ReadAll(ret.Body)

	return string(b[:]), nil
}
