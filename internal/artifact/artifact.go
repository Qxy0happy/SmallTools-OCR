package artifact

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func ZipPathFor(src string) string {
	return src + ".zip"
}

func Validate(path string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	if len(r.File) == 0 {
		return fmt.Errorf("artifact: empty zip")
	}
	for _, f := range r.File {
		if filepath.Base(f.Name) == "markdown.md" {
			return nil
		}
	}
	return fmt.Errorf("artifact: missing markdown.md")
}

func Write(partPath, destPath string) error {
	if err := os.Rename(partPath, destPath); err == nil {
		return nil
	}
	in, err := os.Open(partPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(destPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(partPath)
}
