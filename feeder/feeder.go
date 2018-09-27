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
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/containers/image/docker/reference"

	wlk "github.com/kubic-project/container-feeder/walker"
	log "github.com/sirupsen/logrus"
)

// the default path of container-feeder.json config
var configFile = "/etc/container-feeder.json"

// special type to load the container-feeder.json config
type FeederConfig struct {
	Target    string   `json:"feeder-target,omitempty"`
	Whitelist []string `json:"whitelist,omitempty"`
}

// parseWhitelist returns a whitelist with normalized elements.
func parseWhitelist(whitelist []string) ([]string, error) {
	list := []string{}
	for _, w := range whitelist {
		name, tag, err := normalizeNameTag(w)
		if err != nil {
			return nil, fmt.Errorf("error parsing whitelist item '%s': %v", w, err)
		}
		if tag != "" {
			return nil, fmt.Errorf("whitelisting does not support tags: %s", w)
		}
		list = append(list, name)
	}
	return list, nil
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

	whitelist, err := parseWhitelist(config.Whitelist)
	if err != nil {
		return config, err
	}
	config.Whitelist = whitelist

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

// FeederIface is a generalized interface that Container Feeders must implement
type FeederIface interface {
	Images() ([]string, error)
	LoadImage(string) (string, error)
	TagImage(string, []string) error
}

// Feeder includes a concrete object implementing the FeederIface and
// FeederConfig
type Feeder struct {
	feeder FeederIface
	config FeederConfig
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
func NewFeeder() (*Feeder, error) {
	var err error
	f := Feeder{}

	f.config, err = loadConfig()
	if err != nil {
		return nil, err
	}

	switch f.config.Target {
	case "docker":
		log.Debugf("Feeder target '%s': using DockerFeeder", f.config.Target)
		f.feeder, err = NewDockerFeeder()
	case "crio":
		log.Debugf("Feeder target '%s': using CRIOFeeder", f.config.Target)
		f.feeder, err = NewCRIOFeeder()
	default:
		log.Debugf("Feeder target unspecified: raising an error")
		return nil, fmt.Errorf("Unknown feeder type specified %v", err)
	}

	return &f, err
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
	imagesToImport, imagesToImportTags, err := f.imagesToImport(path)
	if err != nil {
		return res, err
	}

	log.Debugf("Images to import: %v", imagesToImport)
	for tag, file := range imagesToImport {
		_, err := f.feeder.LoadImage(file)
		if err != nil {
			log.Warnf("Could not load image %s: %v", file, err)
			res.FailedImports = append(
				res.FailedImports,
				FailedImportError{
					Image: tag,
					Error: err,
				})
		} else {
			err = f.feeder.TagImage(tag, imagesToImportTags[tag])
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

//normalizeNameTag split the image into it's name and tag.
func normalizeNameTag(image string) (string, string, error) {
	// Remove illegal characters when image is "<none>:<none>"
	re := regexp.MustCompile(`<|>`)
	image = re.ReplaceAllString(image, "")
	ref, err := reference.ParseNormalizedNamed(image)
	if err != nil {
		return "", "", fmt.Errorf("error parsing image name '%s': %v", image, err)
	}
	tag := ""
	nt, isTagged := ref.(reference.NamedTagged)
	if isTagged {
		tag = nt.Tag()
	}
	return ref.Name(), tag, nil
}

// isWhitelisted returns true if the image matches any element in the whitelist.
// Notice that the image has the format `repo:tag` while only the repo must
// match a list element.
func isWhitelisted(image string, whitelist []string) (bool, error) {
	if len(whitelist) == 0 {
		return true, nil
	}

	image, _, err := normalizeNameTag(image)
	if err != nil {
		return false, err
	}

	for _, white := range whitelist {
		if white == image {
			return true, nil
		}
	}
	return false, nil
}

// shouldImportImage will check if any tag under newTags is not inside oldTags.
// If any tag is found that matches this condition, we should import the image.
func (f *Feeder) shouldImportImage(oldTags, newTags []string) (bool) {
	for _, newTag := range newTags {
		if !stringInSlice(newTag, oldTags) {
			return true
		}
	}
	return false
}

// imagesToImport computes the RPMs images that have to be loaded into the CRI
// and returns a map with the repotag string as key and the name of the file as
// value and a map with additional repotags.
func (f *Feeder) imagesToImport(path string) (map[string]string, map[string][]string, error) {
	rpmImages := make(map[string]string)
	rpmImageTags := make(map[string][]string)

	currentRpmImages, currentRpmImageTags, err := findRPMImages(path)
	if err != nil {
		return rpmImages, rpmImageTags, err
	}

	images, err := f.feeder.Images()
	if err != nil {
		return rpmImages, rpmImageTags, err
	}
	if len(images) > 0 {
		log.Debugf("Found the following images in the local storage:")
	}
	for _, img := range images {
		log.Debugf("%s", img)
	}

	for rpmImage, _ := range currentRpmImages {
		whitelisted, err := isWhitelisted(rpmImage, f.config.Whitelist)
		if err != nil {
			return nil, nil, err
		}
		if whitelisted == false {
			log.Debugf("Image %s is not whitelisted: ignoring", rpmImage)
		} else {
			if f.shouldImportImage(images, currentRpmImageTags[rpmImage]) {
				// The image is whitelisted and has not been imported yet
				log.Debugf("Image %s is whitelisted: marking as to be imported", rpmImage)
				rpmImages[rpmImage] = currentRpmImages[rpmImage]
				rpmImageTags[rpmImage] = currentRpmImageTags[rpmImage]
			} else {
				log.Debugf("Image %s is whitelisted but has already been imported", rpmImage)
			}
		}
	}

	log.Debugf("Images to be imported %+v", rpmImageTags)

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

	log.Debugf("Found the following RPM images: %+v", images)
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

	normalizedName, _, err := normalizeNameTag(metadata.Image.Name)
	if err != nil {
		return "", nil, "", err
	}

	repotag := normalizedName + ":" + metadata.Image.Tags[0]
	image := metadata.Image.File

	repotags := make([]string, 0)

	for _, tag := range metadata.Image.Tags[1:] {
		repotags = append(repotags, normalizedName+":"+tag)
	}

	return repotag, repotags, image, nil
}

// runCommand executes the program specified in args with env and writes to
// stdout.
func runCommand(args []string, env string, stdout *os.File) error {
	var cmd *exec.Cmd
	var serr bytes.Buffer

	log.Debugf("runCommand(args=%s, env=%s)", args, env)

	cmd = exec.Command(args[0], args[1:]...)
	cmd.Stdout = stdout
	cmd.Stderr = &serr
	cmd.Env = []string{env}

	err := cmd.Run()
	if err != nil {
		log.Debugf("Error executing command: %s", serr.String())
		return fmt.Errorf("error running command: %s", err.Error())
	}
	return nil
}
