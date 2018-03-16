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
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"github.com/containers/image/docker/reference"
	"github.com/containers/storage"
	"github.com/containers/storage/pkg/reexec"
	"github.com/projectatomic/libpod/libpod"

	log "github.com/sirupsen/logrus"
)

// CRIOFeeder wraps the libpod.Runtime and implementes the Feeder interface.
type CRIOFeeder struct {
	runtime *libpod.Runtime
}

// NewCRIOFeeder returns a pointer to an initialized CRIOFeeder.
func NewCRIOFeeder() (*CRIOFeeder, error) {
	feeder := &CRIOFeeder{}

	if reexec.Init() {
		return nil, fmt.Errorf("could not init CRIOFeeder")
	}

	options := []libpod.RuntimeOption{}
	storageOpts := storage.DefaultStoreOptions
	options = append(options, libpod.WithStorageConfig(storageOpts))

	runtime, err := libpod.NewRuntime(options...)
	if err != nil {
		return nil, fmt.Errorf("error getting libpod runtime: %v", err)
	}

	feeder.runtime = runtime
	return feeder, nil
}

// Images returns an array of images present in containers/storage.
func (f *CRIOFeeder) Images() ([]string, error) {
	tags := []string{}

	images, err := f.runtime.GetImageResults()
	if err != nil {
		return nil, fmt.Errorf("error getting images from libpod: %v", err)
	}

	for _, img := range images {
		tags = append(tags, img.RepoTags...)
	}

	return tags, nil
}

// decompressXZImage decompresses the specified tar.xz into a tar image that is
// located in /var/tmp (writable on MicroOS).
func decompressXZImage(image string) (string, error) {
	log.Debugf("Decompressing image %s", image)

	tmpFile, err := ioutil.TempFile("/var/tmp", "container-feeder")
	if err != nil {
		return "", fmt.Errorf("error creating temporary file: %v", err)
	}
	defer tmpFile.Close()

	cmd := []string{"/usr/bin/unxz", "-c", image}
	if err := runCommand(cmd, "", tmpFile); err != nil {
		return "", fmt.Errorf("error using xz: %v", err)
	}

	return tmpFile.Name(), nil
}

// LoadImage loads the specified image into containers/storage and returns the
// image name.
func (f *CRIOFeeder) LoadImage(path string) (string, error) {
	var writer io.Writer
	options := libpod.CopyOptions{
		Writer: writer,
	}

	image, err := decompressXZImage(path)
	if err != nil {
		return "", err
	}
	defer os.Remove(image)

	src := libpod.DockerArchive + ":" + image
	imgName, err := f.runtime.PullImage(src, options)
	if err != nil {
		return "", fmt.Errorf("error loading image: %v", err)
	}

	log.Debugf("Loaded image: %v", imgName)
	return imgName, nil
}

// TagImage tags the specified image with the supplied tags.
func (f *CRIOFeeder) TagImage(image string, tags []string) error {
	newImage := f.runtime.NewImage(image)
	newImage.GetLocalImageName()

	img, err := f.runtime.GetImage(newImage.LocalName)
	if err != nil {
		return err
	}
	if img == nil {
		return fmt.Errorf("null image")
	}
	log.Debugf("Tagging %s as %v", image, tags)
	err = f.addImageNames(img, tags)
	if err != nil {
		return fmt.Errorf("error tagging image: %v", err)
	}
	return nil
}

// addImageNames adds addNames to the specified image
func (f *CRIOFeeder) addImageNames(image *storage.Image, names []string) error {
	// Add tags to the names if applicable
	tags, err := expandedTags(names)
	if err != nil {
		return err
	}
	for _, tag := range tags {
		if err := f.runtime.TagImage(image, tag); err != nil {
			return fmt.Errorf("error adding name (%v) to image %q", tag, image.ID)
		}
	}
	return nil
}

// expandedTags the specified tags based on their references
func expandedTags(tags []string) ([]string, error) {
	expandedNames := []string{}
	for _, tag := range tags {
		var labelName string
		name, err := reference.Parse(tag)
		if err != nil {
			return nil, fmt.Errorf("error parsing tag %q", name)
		}
		if _, ok := name.(reference.NamedTagged); ok {
			labelName = name.String()
		} else {
			labelName = name.String() + ":latest"
		}
		expandedNames = append(expandedNames, labelName)
	}
	return expandedNames, nil
}
