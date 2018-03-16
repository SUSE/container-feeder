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
	"testing"
)


func TestNormalizeNameTag(t *testing.T) {
	var name, tag string
	var err error

	name, tag, err = normalizeNameTag("opensuse:latest")
	if err != nil || name != "docker.io/library/opensuse" || tag != "latest" {
		t.Errorf("unexpected output: name: %s, tag: %s, err: %v", name, tag, err)
	}

	name, tag, err = normalizeNameTag("opensuse/with/path:latest")
	if err != nil || name != "docker.io/opensuse/with/path" || tag != "latest" {
		t.Errorf("unexpected output: name: %s, tag: %s, err: %v", name, tag, err)
	}

	name, tag, err = normalizeNameTag("registry.suse.com/sles12/foo:bar")
	if err != nil || name != "registry.suse.com/sles12/foo" || tag != "bar" {
		t.Errorf("unexpected output: name: %s, tag: %s, err: %v", name, tag, err)
	}

	name, tag, err = normalizeNameTag("localhost:5000/notag")
	if err != nil || name != "localhost:5000/notag" || tag != "" {
		t.Errorf("unexpected output: name: %s, tag: %s, err: %v", name, tag, err)
	}

	name, tag, err = normalizeNameTag("invalidtag:")
	if err == nil {
		t.Errorf("unexpected output: name: %s, tag: %s, err: %v", name, tag, err)
	}
}

func TestParseWhitelist(t *testing.T) {
	var list, res []string
	var err error

	list = []string{"opensuse", "opensuse/amd64"}
	res, err = parseWhitelist(list)
	if err != nil {
		t.Error("unexpected error: %v", err)
	}
	if res[0] != "docker.io/library/opensuse" {
		t.Error("unexpected list element: %s", res[0])
	}

	list = []string{"registry.suse.com/coolimage:withtag"}
	_, err = parseWhitelist(list)
	if err == nil {
		t.Error("error expected but not received")
	}
}

func TestIsWhitelisted(t *testing.T) {
	whitelist := []string{"docker.io/library/opensuse", "docker.io/sles12/with/a/path"}
	var err error
	var whitelisted bool

	whitelisted, err = isWhitelisted("opensuse:12345", whitelist)
	if err != nil {
		t.Error("Whitelisting should not have failed")
	}
	if whitelisted != true {
		t.Error("Image should be whitelisted")
	}

	whitelisted, err = isWhitelisted("sles12/with/a/path:awesometag", whitelist)
	if err != nil {
		t.Error("Whitelisting should not have failed")
	}
	if whitelisted != true {
		t.Error("Image should be whitelisted")
	}

	whitelisted, err = isWhitelisted("sles12/with/another/path:awesometag", whitelist)
	if err != nil {
		t.Error("Whitelisting should not have failed")
	}
	if whitelisted != false {
		t.Error("Image should not be whitelisted")
	}

	whitelisted, err = isWhitelisted("un:expected:format", whitelist)
	if err == nil {
		t.Error("Whitelisting should not have failed")
	}

	whitelisted, err = isWhitelisted("no:whitelist", []string{})
	if err != nil {
		t.Error("Whitelisting should not have failed")
	}
	if whitelisted != true {
		t.Error("Image should be whitelisted")
	}
}
