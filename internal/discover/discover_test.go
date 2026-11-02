package discover

import (
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// hidden dir
	os.MkdirAll(filepath.Join(dir, ".hidden"), 0755)
	os.WriteFile(filepath.Join(dir, ".hidden", "x.pdf"), []byte("x"), 0644)
	// supported files
	os.WriteFile(filepath.Join(dir, "a.PDF"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(dir, "b.DOCX"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("x"), 0644)
	// symlink file
	os.Symlink(filepath.Join(dir, "a.PDF"), filepath.Join(dir, "link.pdf"))
	// unreadable dir
	bad := filepath.Join(dir, "bad")
	os.MkdirAll(bad, 0755)
	os.WriteFile(filepath.Join(bad, "y.pdf"), []byte("x"), 0644)
	os.Chmod(bad, 0000)
	t.Cleanup(func() { os.Chmod(bad, 0755) })
	return dir
}

func TestFindBasic(t *testing.T) {
	dir := setup(t)
	files, err := Find([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// expect a.PDF, b.DOCX (not hidden, not symlink, not txt)
	if len(files) != 3 {
		t.Fatalf("got %d: %v", len(files), files)
	}
}

func TestFindFileRoot(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.pdf")
	os.WriteFile(p, []byte("x"), 0644)
	files, err := Find([]string{p}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d", len(files))
	}
}

func TestFindDedupAndSort(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "b.pdf"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(dir, "a.pdf"), []byte("x"), 0644)
	files, err := Find([]string{dir, dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Fatalf("got %d", len(files))
	}
	if files[0] > files[1] {
		t.Fatalf("not sorted: %v", files)
	}
}

func TestFindSymlinkDir(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	os.MkdirAll(real, 0755)
	os.WriteFile(filepath.Join(real, "x.pdf"), []byte("x"), 0644)
	os.Symlink(real, filepath.Join(dir, "linkdir"))
	files, err := Find([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// should only find one (not traverse symlink dir duplicate)
	if len(files) != 1 {
		t.Fatalf("got %d: %v", len(files), files)
	}
}
