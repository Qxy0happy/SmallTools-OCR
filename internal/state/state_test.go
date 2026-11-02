package state

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFingerprintStableAndSensitive(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.bin")
	os.WriteFile(p, []byte("hello"), 0o644)
	h1, s1, err := Fingerprint(p)
	if err != nil {
		t.Fatal(err)
	}
	h2, _, _ := Fingerprint(p)
	if h1 != h2 {
		t.Fatalf("unstable: %s != %s", h1, h2)
	}
	if len(h1) != 32 {
		t.Fatalf("len %d", len(h1))
	}
	os.WriteFile(p, []byte("hellp"), 0o644)
	h3, _, _ := Fingerprint(p)
	if h3 == h1 {
		t.Fatal("expected different hash")
	}
	if s1 != 5 {
		t.Fatalf("size %d", s1)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := &State{SchemaVersion: 1, ToolVersion: "0.1.0", Files: map[string]FileRecord{}}
	s.Files["/x.pdf"] = FileRecord{Size: 10, Hash: "abc", Tier: "advanced", ZipPath: filepath.Join(dir, "x.pdf.zip"), Status: "completed"}
	if err := s.Save(dir); err != nil {
		t.Fatal(err)
	}
	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Files["/x.pdf"].Hash != "abc" {
		t.Fatalf("roundtrip failed: %+v", got.Files["/x.pdf"])
	}
}

func TestCorruptBackup(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, ".mineru-state.json")
	os.WriteFile(p, []byte("{bad json"), 0o644)
	got, err := Load(dir)
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("expected ErrCorrupt, got %v", err)
	}
	if _, err := os.Stat(p + ".bak"); err != nil {
		t.Fatalf("bak missing: %v", err)
	}
	if len(got.Files) != 0 {
		t.Fatal("expected empty")
	}
}

func TestShouldSkip(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "a.zip")
	f, _ := os.Create(zipPath)
	zw := zip.NewWriter(f)
	w, _ := zw.Create("markdown.md")
	w.Write([]byte("hi"))
	zw.Close()
	f.Close()

	s := &State{Files: map[string]FileRecord{}}
	src := filepath.Join(dir, "a.pdf")
	s.Files[src] = FileRecord{Status: "completed", Hash: "H", ZipPath: zipPath}

	if !s.ShouldSkip(src, "H") {
		t.Fatal("should skip")
	}
	if s.ShouldSkip(src, "X") {
		t.Fatal("hash mismatch should not skip")
	}
	s.Files[src] = FileRecord{Status: "completed", Hash: "H", ZipPath: filepath.Join(dir, "nope.zip")}
	if s.ShouldSkip(src, "H") {
		t.Fatal("missing zip should not skip")
	}
	zipPath2 := filepath.Join(dir, "b.zip")
	f2, _ := os.Create(zipPath2)
	zw2 := zip.NewWriter(f2)
	zw2.Create("other.txt")
	zw2.Close()
	f2.Close()
	s.Files[src] = FileRecord{Status: "completed", Hash: "H", ZipPath: zipPath2}
	if s.ShouldSkip(src, "H") {
		t.Fatal("no markdown should not skip")
	}
}

func TestStatePathFileRoot(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "x.pdf")
	os.WriteFile(f, []byte("x"), 0o644)
	p := StatePath(f)
	if p != filepath.Join(dir, ".mineru-state.json") {
		t.Fatalf("got %s", p)
	}
}

func TestLoadMissing(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Files == nil {
		t.Fatal("nil files")
	}
}
