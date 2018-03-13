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

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/kubic-project/container-feeder/feeder"
	log "github.com/sirupsen/logrus"
)

// setLogLevel sets the logrus logging level
func setLogLevel(logLevel string) {
	if logLevel != "" {
		lvl, err := log.ParseLevel(logLevel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Unable to parse logging level: %s\n", logLevel)
			os.Exit(1)
		}
		log.SetLevel(lvl)
	} else {
		log.SetLevel(log.InfoLevel)
	}
}

func main() {
	const defaultImageLocation string = "/usr/share/suse-docker-images/native"

	var dir = flag.String("dir", defaultImageLocation, "Import container images from this directory")
	var logLevel = flag.String("log-level", "info", "Set the logging level (\"debug\"|\"info\"|\"warn\"|\"error\"|\"fatal\")")
	flag.Parse()

	setLogLevel(*logLevel)

	importResp, err := feeder.Import(*dir)
	if err != nil {
		log.Errorf("Something went wrong while importing the images: %v\n", err)
		os.Exit(1)
	}

	if len(importResp.SuccessfulImports) > 0 {
		log.Info("Successfully imported the following images:")
	}
	for _, image := range importResp.SuccessfulImports {
		log.Infof("  - %s", image)
	}

	if len(importResp.FailedImports) > 0 {
		log.Error("The following images failed to be imported:")
	}
	for _, failedImport := range importResp.FailedImports {
		log.Errorf("  - %s with error: %v", failedImport.Image, failedImport.Error)
	}
}
