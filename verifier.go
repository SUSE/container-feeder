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
	"os/exec"
	"strings"
)

// Verifies the file has not been tampered
// The check is done using the information inside of the
// RPM database.
// Returns false if the file is not part of a RPM package.
func Verify(file string) (bool, error) {
	// figure out the name of the package shipping the file
	// the `rpm` command exits with an error when the file is not managed by RPM
	out, err := exec.Command(
		"rpm",
		"-qf",
		file).Output()
	if err != nil {
		return false, err
	}
	rpm := strings.Trim(string(out[:]), "\n")

	// verifies the whole rpm
	// the `rpm` command exits with an error when the verification fails
	_, err = exec.Command(
		"rpm",
		"--verify",
		rpm).Output()

	if err != nil {
		return false, err
	}

	return true, nil
}
