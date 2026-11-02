package artifact

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestZipPathFor(t *testing.T) {
	if got := ZipPathFor("/a/report.pdf"); got != "/a/report.pdf.zip" {
		t.Fatalf("got %s", got)
	}
	if ZipPathFor("x.pdf") == ZipPathFor("x.docx") {
		t.Fatal("collision")
	}
}

func TestValidate(t *testing.T) {
	dir := t.TempDir()
	// empty file
	empty := filepath.Join(dir, "empty.zip")
	os.WriteFile(empty, []byte{}, 0o644)
	if err := Validate(empty); err == nil {
		t.Fatal("expected error for empty")
	}
	// non zip
	nonzip := filepath.Join(dir, "x.txt")
	os.WriteFile(nonzip, []byte("hello"), 0o644)
	if err := Validate(nonzip); err == nil {
		t.Fatal("expected error for non-zip")
	}
	// zip without markdown
	noMd := filepath.Join(dir, "nomd.zip")
	f, _ := os.Create(noMd)
	zw := zip.NewWriter(f)
	zw.Create("other.txt")
	zw.Close()
	f.Close()
	if err := Validate(noMd); err == nil {
		t.Fatal("expected error for missing markdown")
	}
	// valid
	valid := filepath.Join(dir, "valid.zip")
	fv, _ := os.Create(valid)
	zwv := zip.NewWriter(fv)
	w, _ := zwv.Create("markdown.md")
	w.Write([]byte("hi"))
	zwv.Close()
	fv.Close()
	if err := Validate(valid); err != nil {
		t.Fatalf("valid should pass: %v", err)
	}
}

func TestWrite(t *testing.T) {
	dir := t.TempDir()
	part := filepath.Join(dir, "a.part")
	dest := filepath.Join(dir, "a.zip")
	os.WriteFile(part, []byte("data"), 0o644)
	if err := Write(part, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(part); !os.IsNotExist(err) {
		t.Fatal("part should be removed")
	}
}
