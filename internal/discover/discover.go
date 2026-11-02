package discover

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var exts = map[string]bool{
	".pdf":  true,
	".doc":  true,
	".docx": true,
	".ppt":  true,
	".pptx": true,
}

func Find(roots []string, warn func(string)) ([]string, error) {
	if warn == nil {
		warn = func(string) {}
	}
	seenRoots := map[string]bool{}
	var all []string
	seenFiles := map[string]bool{}

	for _, r := range roots {
		abs, err := filepath.Abs(r)
		if err != nil {
			return nil, err
		}
		abs = filepath.Clean(abs)
		if seenRoots[abs] {
			continue
		}
		seenRoots[abs] = true

		info, err := os.Stat(abs)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if exts[strings.ToLower(filepath.Ext(abs))] {
				if !seenFiles[abs] {
					seenFiles[abs] = true
					all = append(all, abs)
				}
			}
			continue
		}
		// walk
		err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				warn(fmt.Sprintf("cannot read %s: %v", path, err))
				return filepath.SkipDir
			}
			if path == abs {
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") {
					return filepath.SkipDir
				}
				return nil
			}
			if exts[strings.ToLower(filepath.Ext(d.Name()))] {
				p := filepath.Clean(path)
				if !seenFiles[p] {
					seenFiles[p] = true
					all = append(all, p)
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}

	sort.Strings(all)
	return all, nil
}
