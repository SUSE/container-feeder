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
	"fmt"

	"github.com/containers/common/libimage"
	"github.com/containers/image/v5/docker/reference"
	"github.com/containers/podman/v4/libpod"
	"github.com/containers/storage/pkg/reexec"

	log "github.com/sirupsen/logrus"
)

// CRIOFeeder wraps the libpod.Runtime and implementes the Feeder interface.
type CRIOFeeder struct {
	runtime           *libpod.Runtime
	backgroundContext context.Context
}

// NewCRIOFeeder returns a pointer to an initialized CRIOFeeder.
func NewCRIOFeeder() (*CRIOFeeder, error) {
	feeder := &CRIOFeeder{backgroundContext: context.Background()}

	if reexec.Init() {
		return nil, fmt.Errorf("could not init CRIOFeeder")
	}

	runtime, err := libpod.NewRuntime(feeder.backgroundContext)
	if err != nil {
		return nil, fmt.Errorf("error getting libpod runtime: %v", err)
	}

	feeder.runtime = runtime
	return feeder, nil
}

// Images returns an array of images present in containers/storage.
func (f *CRIOFeeder) Images() ([]string, error) {
	tags := []string{}

	imgs := []string{}
	images, err := f.runtime.LibimageRuntime().ListImages(f.backgroundContext, imgs, nil)
	if err != nil {
		return nil, fmt.Errorf("error getting images from libpod: %v", err)
	}

	for _, img := range images {
		imgTags, err := img.RepoTags()
		if err != nil {
			return nil, err
		}
		tags = append(tags, imgTags...)
	}

	return tags, nil
}

// LoadImage loads the specified image into containers/storage and returns the
// image name.
func (f *CRIOFeeder) LoadImage(path string) (string, error) {
	imgs, err := f.runtime.LibimageRuntime().Load(f.backgroundContext, path, nil)
	if err != nil {
		return "", err
	}

	log.Debugf("Loaded image(s): %v", imgs)
	return imgs[0], nil
}

// TagImage tags the specified image with the supplied tags.
func (f *CRIOFeeder) TagImage(image string, tags []string) error {
	img, _, err := f.runtime.LibimageRuntime().LookupImage(image, nil)
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
func (f *CRIOFeeder) addImageNames(image *libimage.Image, names []string) error {
	// Add tags to the names if applicable
	tags, err := expandedTags(names)
	if err != nil {
		return err
	}
	for _, tag := range tags {
		if err := image.Tag(tag); err != nil {
			return fmt.Errorf("error adding name (%v) to image %q", tag, image.ID())
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
