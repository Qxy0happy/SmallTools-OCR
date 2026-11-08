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
	os.MkdirAll(filepath.Join(dir, ".hidden"), 0o755)
	os.WriteFile(filepath.Join(dir, ".hidden", "x.pdf"), []byte("x"), 0o644)
	// supported files
	os.WriteFile(filepath.Join(dir, "a.PDF"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.DOCX"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("x"), 0o644)
	// symlink file
	os.Symlink(filepath.Join(dir, "a.PDF"), filepath.Join(dir, "link.pdf"))
	// unreadable dir
	bad := filepath.Join(dir, "bad")
	os.MkdirAll(bad, 0o755)
	os.WriteFile(filepath.Join(bad, "y.pdf"), []byte("x"), 0o644)
	os.Chmod(bad, 0o000)
	t.Cleanup(func() { os.Chmod(bad, 0o755) })
	return dir
}

func TestFindBasic(t *testing.T) {
	dir := setup(t)
	var warned []string
	files, err := Find([]string{dir}, func(msg string) { warned = append(warned, msg) })
	if err != nil {
		t.Fatal(err)
	}
	// expect a.PDF, b.DOCX (not hidden, not symlink, not txt, not in unreadable dir)
	if len(files) != 2 {
		t.Fatalf("got %d: %v", len(files), files)
	}
	if len(warned) != 1 {
		t.Fatalf("warnings = %v, want 1 warning for the unreadable dir", warned)
	}
}

func TestFindFileRoot(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x.pdf")
	os.WriteFile(p, []byte("x"), 0o644)
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
	os.WriteFile(filepath.Join(dir, "b.pdf"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, "a.pdf"), []byte("x"), 0o644)
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
	realDir := filepath.Join(dir, "real")
	os.MkdirAll(realDir, 0o755)
	os.WriteFile(filepath.Join(realDir, "x.pdf"), []byte("x"), 0o644)
	os.Symlink(realDir, filepath.Join(dir, "linkdir"))
	files, err := Find([]string{dir}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// should only find one (not traverse symlink dir duplicate)
	if len(files) != 1 {
		t.Fatalf("got %d: %v", len(files), files)
	}
}
