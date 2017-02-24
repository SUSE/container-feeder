package main

import (
	"os"
	"path/filepath"
	"strings"
)

// List the files inside of a directory.
// Note well: doesn't walk recursively
// It's possible to list only the files matching a given extension,
// remember to add the "." (eg: ".mp3")
type Walker struct {
	Root      string
	Extension string // only list files with this extension
	Files     []string
}

func NewWalker(path, extension string) *Walker {
	return &Walker{
		Files:     []string{},
		Extension: extension,
		Root:      path,
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
		}

		if add {
			w.Files = append(w.Files, filepath.Base(path))
		}
	}

	return nil
}
