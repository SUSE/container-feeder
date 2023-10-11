/*
 * container-feeder: import Linux container images delivered as RPMs
 * Copyright 2018 SUSE LLC
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

package feeder

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/client"

	log "github.com/sirupsen/logrus"
)

type DockerFeeder struct {
	client *client.Client
}

// Returns a new Feeder instance. Takes care of initializing the connection
// with the Docker daemon.
func NewDockerFeeder() (*DockerFeeder, error) {
	feeder := &DockerFeeder{}

	var err error
	feeder.client, err = connectToDaemon()
	if err != nil {
		return &DockerFeeder{}, err
	}

	return feeder, nil
}

// connectToDaemon returns a Docker client.Client using the right version of
// the API
func connectToDaemon() (*client.Client, error) {
	// Set the exact version of the API in use, otherwise the library will
	// try to use the latest one, which might be too new compared to the
	// one supported by the docker daemon

	cli, err := client.NewEnvClient()
	if err != nil {
		return nil, err
	}
	vers, err := cli.ServerVersion(context.Background())
	if err != nil {
		return nil, err
	}

	var apiVersion string = ""

	for _, component := range vers.Components {
		if component.Name != "Engine" {
			continue
		}
		if vers, found := component.Details["ApiVersion"]; found {
			apiVersion = vers
		}
	}

	if apiVersion == "" {
		return nil, errors.New("Could not obtain docker Engine API version")
	}

	if err := os.Setenv("DOCKER_API_VERSION", apiVersion); err != nil {
		return nil, err
	}

	cli, err = client.NewEnvClient()

	if err != nil {
		return nil, err
	}

	return cli, nil
}

// Images returns images available on the docker host in the form
// "<repo>:<tag>".
func (f *DockerFeeder) Images() ([]string, error) {
	tags := []string{}
	images, err := f.client.ImageList(context.Background(), types.ImageListOptions{})
	if err != nil {
		return tags, err
	}

	for _, image := range images {
		for _, tag := range image.RepoTags {
			normalizedName, normalizedTag, err := normalizeNameTag(tag)
			if err != nil {
				return []string{}, err
			}
			tags = append(tags, normalizedName+":"+normalizedTag)
		}
	}

	return tags, nil
}

type loadResponse struct {
	Stream string `json:stream`
}

// LoadImage loads the specified image into docker. Returns the image name
// loaded into the docker daemon.
func (f *DockerFeeder) LoadImage(pathToImage string) (string, error) {
	image, err := os.Open(pathToImage)
	if err != nil {
		return "", err
	}
	defer image.Close()

	ret, err := f.client.ImageLoad(context.Background(), image, true)
	if err != nil {
		return "", err
	}
	defer ret.Body.Close()
	b, err := io.ReadAll(ret.Body)
	if err != nil {
		return "", err
	}

	loadResp := loadResponse{}
	err = json.Unmarshal(b, &loadResp)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(strings.TrimPrefix(string(loadResp.Stream), "Loaded image:")), nil
}

// TagImage tags the specified docker image with the supplied tags.
func (f *DockerFeeder) TagImage(image string, tags []string) error {
	for _, tag := range tags {
		log.Debug("Tagging image: ", image, " with ", tag)
		if err := f.client.ImageTag(context.Background(), image, tag); err != nil {
			return err
		}
	}
	return nil
}
