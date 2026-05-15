package sandboxfs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestListReturnsSortedEntries(t *testing.T) {
	w := New(fstest.MapFS{
		"b.txt":        {Data: []byte("b")},
		"a.txt":        {Data: []byte("a")},
		"dir/c.txt":    {Data: []byte("c")},
		"dir/nested/d": {Data: []byte("d")},
	})

	result, err := w.List(context.Background(), ListRequest{Path: ".", Recursive: true})
	if err != nil {
		t.Fatal(err)
	}

	got := entryPaths(result.Entries)
	want := []string{"a.txt", "b.txt", "dir", "dir/c.txt", "dir/nested", "dir/nested/d"}
	if !sameStrings(got, want) {
		t.Fatalf("paths = %#v, want %#v", got, want)
	}
}

func TestListRejectsTraversal(t *testing.T) {
	w := New(fstest.MapFS{"safe.txt": {Data: []byte("safe")}})

	_, err := w.List(context.Background(), ListRequest{Path: "../"})
	if !errors.Is(err, ErrInvalidPath) {
		t.Fatalf("error = %v, want ErrInvalidPath", err)
	}
}

func TestReadSupportsLineRanges(t *testing.T) {
	w := New(fstest.MapFS{
		"notes.txt": {Data: []byte("one\ntwo\nthree\nfour\n")},
	})

	result, err := w.Read(context.Background(), ReadRequest{
		Path:      "notes.txt",
		StartLine: 2,
		LineCount: 2,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Text != "two\nthree" {
		t.Fatalf("text = %q, want line range", result.Text)
	}
	if result.StartLine != 2 || result.EndLine != 3 {
		t.Fatalf("line range = %d-%d, want 2-3", result.StartLine, result.EndLine)
	}
	if !result.Truncated {
		t.Fatal("truncated = false, want true for partial range")
	}
}

func TestReadRejectsBinaryFiles(t *testing.T) {
	w := New(fstest.MapFS{
		"bin.dat": {Data: []byte{'o', 'k', 0, 'x'}},
	})

	_, err := w.Read(context.Background(), ReadRequest{Path: "bin.dat"})
	if !errors.Is(err, ErrBinaryFile) {
		t.Fatalf("error = %v, want ErrBinaryFile", err)
	}
}

func TestReadAppliesByteLimit(t *testing.T) {
	w := New(fstest.MapFS{
		"large.txt": {Data: []byte("abcdef")},
	})

	result, err := w.Read(context.Background(), ReadRequest{Path: "large.txt", MaxBytes: 3})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "abc" {
		t.Fatalf("text = %q, want %q", result.Text, "abc")
	}
	if !result.Truncated {
		t.Fatal("truncated = false, want true")
	}
}

func TestGrepFindsLiteralMatchesCaseInsensitive(t *testing.T) {
	w := New(fstest.MapFS{
		"a.txt":     {Data: []byte("Alpha\nbeta\n")},
		"dir/b.txt": {Data: []byte("gamma\nALPHA here\n")},
		"bin.dat":   {Data: []byte{'a', 0, 'b'}},
	})

	result, err := w.Grep(context.Background(), GrepRequest{Query: "alpha"})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Matches) != 2 {
		t.Fatalf("match count = %d, want 2", len(result.Matches))
	}
	if result.Matches[0].Path != "a.txt" || result.Matches[0].Line != 1 || result.Matches[0].Column != 1 {
		t.Fatalf("first match = %#v", result.Matches[0])
	}
	if result.Matches[1].Path != "dir/b.txt" || result.Matches[1].Line != 2 || result.Matches[1].Column != 1 {
		t.Fatalf("second match = %#v", result.Matches[1])
	}
}

func TestGrepSupportsRegex(t *testing.T) {
	w := New(fstest.MapFS{
		"main.go": {Data: []byte("func main() {}\nvar x = 1\n")},
	})

	result, err := w.Grep(context.Background(), GrepRequest{
		Query:         `func\s+\w+`,
		Regex:         true,
		CaseSensitive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 1 {
		t.Fatalf("match count = %d, want 1", len(result.Matches))
	}
}

func TestGrepRespectsMaxMatches(t *testing.T) {
	w := New(fstest.MapFS{
		"a.txt": {Data: []byte("needle\nneedle\nneedle\n")},
	})

	result, err := w.Grep(context.Background(), GrepRequest{Query: "needle", MaxMatches: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Matches) != 2 {
		t.Fatalf("match count = %d, want 2", len(result.Matches))
	}
	if !result.Truncated {
		t.Fatal("truncated = false, want true")
	}
}

func TestNewSnapshotCopiesRegularFiles(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "a.txt"), []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(source, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "dir", "b.txt"), []byte("world"), 0o600); err != nil {
		t.Fatal(err)
	}

	w, root, err := NewSnapshot(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(root)

	result, err := w.Read(context.Background(), ReadRequest{Path: "dir/b.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "world" {
		t.Fatalf("text = %q, want world", result.Text)
	}

	if err := os.WriteFile(filepath.Join(source, "dir", "b.txt"), []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err = w.Read(context.Background(), ReadRequest{Path: "dir/b.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Text != "world" {
		t.Fatalf("snapshot text = %q, want unchanged world", result.Text)
	}
}

func TestNewSnapshotRejectsSymlinks(t *testing.T) {
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "target.txt"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(source, "target.txt"), filepath.Join(source, "link.txt")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	_, _, err := NewSnapshot(context.Background(), source)
	if err == nil {
		t.Fatal("NewSnapshot succeeded, want symlink error")
	}
}

func entryPaths(entries []Entry) []string {
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}
	return paths
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
