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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	wlk "github.com/kubic-project/container-feeder/walker"
	log "github.com/sirupsen/logrus"
)

// the default path of container-feeder.json config
var configFile = "/etc/container-feeder.json"

// special type to load the container-feeder.json config
type FeederConfig struct {
	Target string `json:"feeder-target,omitempty"`
}

// loadConfig loads and returns the container-feeder.json config
func loadConfig() (FeederConfig, error) {
	config := FeederConfig{}

	file, err := ioutil.ReadFile(configFile)
	if err != nil {
		return config, err
	}

	if err := json.Unmarshal(file, &config); err != nil {
		return config, err
	}

	return config, nil
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

// Feeder is a generalized interface that Container Feeders must implement
type Feeder interface {
	Images() ([]string, error)
	LoadImage(string) (string, error)
	TagImage(string, []string) error
}

// stringInSlice returns true if a is in list.
func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

// NewFeeder returns a new Container Feeder based on the specified type in
// the container-feeder.json config (default: DockerFeeder)
func NewFeeder() (Feeder, error) {
	config, err := loadConfig()
	if err != nil {
		return nil, err
	}
	switch config.Target {
	case "docker":
		log.Debugf("Feeder target '%s': using DockerFeeder", config.Target)
		return NewDockerFeeder()
	case "crio":
		log.Debugf("Feeder target '%s': using CRIOFeeder", config.Target)
		return NewCRIOFeeder()
	default:
		log.Debugf("Feeder target unspecified: defaulting to DockerFeeder")
		return NewDockerFeeder()
	}
}

// Imports all the RPMs images stored inside of `path` into
// the local docker daemon
func Import(path string) (FeederLoadResponse, error) {
	res := FeederLoadResponse{}

	f, err := NewFeeder()
	if err != nil {
		return res, fmt.Errorf("Error creating new feeder: %v", err)
	}

	log.Debugf("Trying to import images from %s", path)
	imagesToImport, imagesToImportTags, err := imagesToImport(f, path)
	if err != nil {
		return res, err
	}

	log.Debugf("Images to import: %v", imagesToImport)
	for tag, file := range imagesToImport {
		_, err := f.LoadImage(file)
		if err != nil {
			log.Warnf("Could not load image %s: %v", file, err)
			res.FailedImports = append(
				res.FailedImports,
				FailedImportError{
					Image: tag,
					Error: err,
				})
		} else {
			err = f.TagImage(tag, imagesToImportTags[tag])
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

// imagesToImport computes the RPMs images that have to be loaded into Docker
// and returns a map with the repotag string as key and the name of the file as
// value and a map with additional repotags.
func imagesToImport(f Feeder, path string) (map[string]string, map[string][]string, error) {
	rpmImages, rpmImageTags, err := findRPMImages(path)
	if err != nil {
		return rpmImages, rpmImageTags, err
	}

	images, err := f.Images()
	if err != nil {
		return rpmImages, rpmImageTags, err
	}

	for rpmImage, _ := range rpmImages {
		needsImport := false

		if stringInSlice(rpmImage, images) == false {
			needsImport = true
		}

		for _, additionalTag := range rpmImageTags[rpmImage] {
			if stringInSlice(additionalTag, images) == false {
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
	log.Debugf("Searching images in %s", path)
	walker := wlk.NewWalker(path, ".metadata")
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

	log.Debugf("Found the following RPM images: %v", images)
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
