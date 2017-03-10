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
	"path/filepath"

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
	res := FeederLoadResponse{}

	imagesToImport, err := f.imagesToImport(path)
	if err != nil {
		return res, err
	}

	for tag, file := range imagesToImport {
		_, err := loadDockerImage(f.dockerClient, file)
		if err != nil {
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

	return res, nil
}

// Computes the RPMs images that have to be loaded into Docker
// Returns a map with the repotag string as key and the name of the file as value
func (f *Feeder) imagesToImport(path string) (map[string]string, error) {
	rpmImages, err := findRPMImages(path)
	if err != nil {
		return rpmImages, err
	}

	dockerImages, err := existingImages(f.dockerClient)
	if err != nil {
		return rpmImages, err
	}

	for _, dockerImage := range dockerImages {
		// ignore the tags that are already known by docker
		delete(rpmImages, dockerImage)
	}

	return rpmImages, nil
}

// Finds all the Docker images shipped by RPMs
// Returns a map with the repotag string as key and the full path to the
// file as value.
func findRPMImages(path string) (map[string]string, error) {
	walker := NewWalker(path, ".xz")
	images := make(map[string]string)

	if err := filepath.Walk(path, walker.Scan); err != nil {
		return images, err
	}

	for _, file := range walker.Files {
		// TODO: extract the repotag from the file
		images[repotagFromRPMFile(file)] = filepath.Join(path, file)
	}

	return images, nil
}

// Compute the repotag (`<name>:<tag>`) starting from the name of the tar.xz
// file shipped by RPM
func repotagFromRPMFile(file string) string {
	// TODO: Implent code once the schema of the filename is defined
	return file
}
