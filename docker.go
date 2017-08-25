/*
 * container-feeder: import Linux container images delivered as RPMs
 * Copyright 2017 SUSE LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"encoding/json"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/net/context"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"

	log "github.com/Sirupsen/logrus"
)

// Figure the API version supported by the server
// by shelling out.
func dockerDaemonAPIVersion() (string, error) {
	out, err := exec.Command(
		"docker",
		"version",
		"--format",
		"{{.Server.APIVersion}}").Output()
	if err != nil {
		return "", err
	}
	api := strings.Trim(string(out[:]), "\n")
	return api, nil
}

// Connects to the local daemon using the right version of the API
func connectToDaemon() (*client.Client, error) {
	// Set the exact version of the API in use, otherwise the library will
	// try to use the latest one, which might be too newer compared to the
	// one supported by the docker daemon

	apiVersion, err := dockerDaemonAPIVersion()
	if err != nil {
		return nil, err
	}
	if err := os.Setenv("DOCKER_API_VERSION", apiVersion); err != nil {
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

type LoadResponseBody struct {
	Stream string `json:"stream"`
}

// Loads the specified image into docker
// Returns the image name loaded into the docker daemon
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

	// {"stream":"Loaded image: sles12/mariadb:10.0\n"}
	var loadResponseBody LoadResponseBody
	if err := json.Unmarshal(b[:], &loadResponseBody); err != nil {
		return "", err
	}
	return strings.TrimSpace(strings.TrimPrefix(
		loadResponseBody.Stream, "Loaded image:")), nil
}

// Tags the specified docker image with the supplied tags
func tagDockerImage(cli *client.Client, image string, tags []string) error {
	for _, tag := range tags {
		log.Debug("Tagging image: ", image, " with ", tag)

		qualifiedTag := strings.Split(image, ":")[0] + ":" + tag

		err := cli.ImageTag(context.Background(), image, qualifiedTag)
		if err != nil {
			return err
		}
	}

	return nil
}
