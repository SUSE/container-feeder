package walker

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestWalkerFailVerificationFileNotManagedByRPM(t *testing.T) {
	topDir, err := os.MkdirTemp("", "test-walker")
	if err != nil {
		t.Errorf("error while creating test dir: %v", err)
	}
	// cleanup dir
	defer os.RemoveAll(topDir)

	f, err := os.Create(filepath.Join(topDir, "foo.mp3"))
	err = f.Close()
	if err != nil {
		t.Errorf("error closing file: %v", err)
	}

	walker := NewWalker(topDir, ".mp3")
	if err := filepath.Walk(topDir, walker.Scan); err != nil {
		t.Errorf("walker error: %v", err)
	}

	if len(walker.Files) != 0 {
		t.Error("Expected 0 matches")
	}
}

func TestWalkerWithVerificationEnabled(t *testing.T) {
	topDir := "/usr/share/zoneinfo"

	if _, err := os.Stat(topDir); os.IsNotExist(err) {
		t.Skipf("skipped: %s does not exist", topDir)
	}

	walker := NewWalker(topDir, ".tab")
	if err := filepath.Walk(topDir, walker.Scan); err != nil {
		t.Errorf("walker error: %v", err)
	}

	if _, err := os.Stat("/bin/rpm"); os.IsNotExist(err) {
		t.Skip("skipped: rpm binary not found in /bin/rpm")
	}

	if len(walker.Files) == 0 {
		t.Error("Expected multiple matches")
	}
}

func TestWalker(t *testing.T) {
	topDir, err := os.MkdirTemp("", "test-walker")
	if err != nil {
		t.Errorf("error while creating test dir: %v", err)
	}
	// cleanup dir
	defer os.RemoveAll(topDir)

	subDir, err := os.MkdirTemp(topDir, "sub")
	if err != nil {
		t.Errorf("error while creating sub dir: %v", err)
	}

	expected := []string{}

	for i := 0; i < 2; i++ {
		name := fmt.Sprintf("ignored-%d", i)
		f, err := os.Create(filepath.Join(topDir, name))
		if err != nil {
			t.Errorf("error creating file: %v", err)
		}
		err = f.Close()
		if err != nil {
			t.Errorf("error closing file: %v", err)
		}
	}

	for i := 0; i < 2; i++ {
		name := fmt.Sprintf("expected-%d.mp3", i)
		f, err := os.Create(filepath.Join(topDir, name))
		if err != nil {
			t.Errorf("error creating file: %v", err)
		}
		err = f.Close()
		if err != nil {
			t.Errorf("error closing file: %v", err)
		}
		expected = append(expected, name)
	}

	for i := 0; i < 2; i++ {
		name := fmt.Sprintf("sub-ignored-%d.mp3", i)
		f, err := os.Create(filepath.Join(subDir, name))
		if err != nil {
			t.Errorf("error creating file: %v", err)
		}
		err = f.Close()
		if err != nil {
			t.Errorf("error closing file: %v", err)
		}
	}

	walker := NewWalker(topDir, ".mp3")
	walker.VerifyFiles = false
	if err := filepath.Walk(topDir, walker.Scan); err != nil {
		t.Errorf("walker error: %v", err)
	}

	actual := walker.Files
	sort.Strings(actual)
	sort.Strings(expected)

	if len(expected) != len(walker.Files) {
		t.Errorf(
			"Different number of files found by walker: expected %v - found %v",
			expected,
			walker.Files)
	}

	for i, e := range expected {
		if actual[i] != e {
			t.Errorf(
				"Different files found by walker: expected %v - found %v",
				expected,
				walker.Files)
		}
	}
}

func TestWalkerNonExistingDirectory(t *testing.T) {
	walker := NewWalker("/boom", ".mp3")
	walker.VerifyFiles = false
	if err := filepath.Walk("/boom", walker.Scan); err == nil {
		t.Errorf("error was expected")
	}
}
