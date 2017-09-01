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
	"path/filepath"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/docker/client"
)

type Feeder struct {
	dockerClient *client.Client
}

type FailedImportError struct {
	Image string
	Error error
}
type FeederLoadResponse struct {
	SuccessfulImports []string
	FailedImports     []FailedImportError
}

/* Image metadata type JSON schema:
{
  "image": {
    "name": "",
    "tags": [ ],
    "file": ""
  }
}
For example:
{
  "image": {
    "name": "opensuse/salt-api",
    "tags": [ "13", "13.0.1", "latest" ],
    "file": "salt-api-2017.03-docker-images.x86_64.tar.xz"
  }
}
*/
// MetadataType struct to handle JSON schema
type MetadataType struct {
	Image ImageType
}

// ImageType struct to handle JSON schema
type ImageType struct {
	Name string   `json:"name"`
	Tags []string `json:"tags"`
	File string   `json:"file"`
}

// Returns a new Feeder instance. Takes care of
// initializing the connection with the Docker daemon
func NewFeeder() (*Feeder, error) {
	cli, err := connectToDaemon()
	if err != nil {
		return &Feeder{}, err
	}

	return &Feeder{
		dockerClient: cli,
	}, nil
}

// Imports all the RPMs images stored inside of `path` into
// the local docker daemon
func (f *Feeder) Import(path string) (FeederLoadResponse, error) {
	log.Debugf("Importing images from %s", path)
	res := FeederLoadResponse{}
	imagesToImport, imagesToImportTags, err := f.imagesToImport(path)
	if err != nil {
		return res, err
	}

	for tag, file := range imagesToImport {
		_, err := loadDockerImage(f.dockerClient, file)
		if err != nil {
			log.Warnf("Could not load image %s: %v", file, err)
			res.FailedImports = append(
				res.FailedImports,
				FailedImportError{
					Image: tag,
					Error: err,
				})
		} else {
			err = tagDockerImage(f.dockerClient, tag, imagesToImportTags[tag])
			if err != nil {
				log.Warnf("Could not tag image %s: %v", file, err)
				res.FailedImports = append(
					res.FailedImports,
					FailedImportError{
						Image: tag,
						Error: err,
					})
			} else {
				res.SuccessfulImports = append(res.SuccessfulImports, tag)
			}
		}
	}

	return res, nil
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

// Computes the RPMs images that have to be loaded into Docker
// Returns a map with the repotag string as key and the name of the file as value
//         and a map with additional repotags
func (f *Feeder) imagesToImport(path string) (map[string]string, map[string][]string, error) {
	rpmImages, rpmImageTags, err := findRPMImages(path)
	if err != nil {
		return rpmImages, rpmImageTags, err
	}

	dockerImages, err := existingImages(f.dockerClient)
	if err != nil {
		return rpmImages, rpmImageTags, err
	}

	for rpmImage, _ := range rpmImages {
		needsImport := false

		if stringInSlice(rpmImage, dockerImages) == false {
			needsImport = true
		}

		for _, additionalTag := range rpmImageTags[rpmImage] {
			if stringInSlice(additionalTag, dockerImages) == false {
				needsImport = true
			}
		}

		if needsImport == false {
			log.Info("Skipping import of ", rpmImage, " all tags exist")
			// ignore the tags that are already known by docker
			delete(rpmImages, rpmImage)
			delete(rpmImageTags, rpmImage)
		}
	}

	return rpmImages, rpmImageTags, nil
}

// Finds all the Docker images shipped by RPMs
// Returns a map with the repotag string as key and the full path to the
// file as value, and a map with additional repotags
func findRPMImages(path string) (map[string]string, map[string][]string, error) {
	log.Debugf("Finding images from %s", path)
	walker := NewWalker(path, ".metadata")
	images := make(map[string]string)
	image_tags := make(map[string][]string)

	if err := filepath.Walk(path, walker.Scan); err != nil {
		return images, image_tags, err
	}

	for _, file := range walker.Files {
		file_path := filepath.Join(path, file)
		repotag, repotags, image, err := repotagFromRPMFile(file_path)
		if err != nil {
			return images, image_tags, err
		}
		// Check if image exist on disk
		image_path := filepath.Join(path, image)
		if _, err := os.Stat(image_path); err == nil {
			images[repotag] = image_path
			image_tags[repotag] = repotags
		} else {
			log.Debugf("Image %s does not exist", image_path)
		}
	}

	return images, image_tags, nil
}

// Compute the repotag (`<name>:<tag>`) starting from the name of the tar.xz
// file shipped by RPM
// Returns repotag (`<name>:<tag>`), a list of additional tags, and image name
func repotagFromRPMFile(file string) (string, []string, string, error) {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return "", nil, "", err
	}

	var metadata MetadataType
	if err := json.Unmarshal(data, &metadata); err != nil {
		return "", nil, "", err
	}

	repotag := metadata.Image.Name + ":" + metadata.Image.Tags[0]
	image := metadata.Image.File

	repotags := make([]string, 0)

	for _, tag := range metadata.Image.Tags[1:] {
		repotags = append(repotags, metadata.Image.Name+":"+tag)
	}

	return repotag, repotags, image, nil
}
