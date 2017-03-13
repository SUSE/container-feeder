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
	"log"
	"os"
	"path/filepath"
	"strings"
)

// List the files inside of a directory.
// Note well: doesn't walk recursively
// It's possible to list only the files matching a given extension,
// remember to add the "." (eg: ".mp3")
type Walker struct {
	Root        string
	Extension   string // only list files with this extension
	Files       []string
	VerifyFiles bool
}

func NewWalker(path, extension string) *Walker {
	return &Walker{
		Files:       []string{},
		Extension:   extension,
		Root:        path,
		VerifyFiles: true,
	}
}

func (w *Walker) Scan(path string, f os.FileInfo, err error) error {
	if err != nil {
		return err
	}

	if f.IsDir() && path != w.Root {
		return filepath.SkipDir
	}

	if path != w.Root {
		add := true

		if w.Extension != "" && strings.ToLower(w.Extension) != strings.ToLower(filepath.Ext(path)) {
			add = false
		} else if w.VerifyFiles {
			var verifyErr error
			add, verifyErr = Verify(path)
			if verifyErr != nil {
				log.Printf("Ignoring file %s because verification failed %v", path, verifyErr)
			}
		}

		if add {
			w.Files = append(w.Files, filepath.Base(path))
		}
	}

	return nil
}
