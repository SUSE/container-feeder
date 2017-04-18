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
	"flag"
	"os"

	log "github.com/Sirupsen/logrus"
)

func main() {
	const defaultImageLocation string = "/usr/share/suse-docker-images/native"

	var dir = flag.String("dir", defaultImageLocation, "directory containing the images to import")
	flag.Parse()

	if *dir == "" {
		log.Error("missing mandatory `--dir` value")
		os.Exit(1)
	}

	feeder, err := NewFeeder()
	if err != nil {
		log.Printf("Something went wrong while initializing the image feeder: %v\n", err)
		os.Exit(1)
	}

	importResp, err := feeder.Import(*dir)
	if err != nil {
		log.Error("Something went wrong while imporing the images: %v\n", err)
		os.Exit(1)
	}

	if len(importResp.SuccessfulImports) > 0 {
		log.Info("Successfully imported the following images:")
	}
	for _, image := range importResp.SuccessfulImports {
		log.Info("  - %s\n", image)
	}

	if len(importResp.FailedImports) > 0 {
		log.Error("The following images failed to be imported:")
	}
	for _, failedImport := range importResp.FailedImports {
		log.Error("  - %s with error: %v\n", failedImport.Image, failedImport.Error)
	}
}
