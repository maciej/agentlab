package sandboxfs

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	DefaultMaxEntries     = 500
	DefaultMaxReadBytes   = 1 << 20
	DefaultMaxMatches     = 100
	DefaultMaxLineBytes   = 256 << 10
	DefaultMaxOutputBytes = 1 << 20
)

var (
	ErrInvalidPath = errors.New("invalid sandbox path")
	ErrNotFile     = errors.New("path is not a regular file")
	ErrNotDir      = errors.New("path is not a directory")
	ErrTooLarge    = errors.New("sandbox output limit exceeded")
	ErrBinaryFile  = errors.New("binary files are not readable")
)

type Workspace struct {
	fsys fs.FS

	maxEntries     int
	maxReadBytes   int64
	maxMatches     int
	maxLineBytes   int
	maxOutputBytes int64
}

type Option func(*Workspace)

func WithMaxEntries(limit int) Option {
	return func(w *Workspace) {
		if limit > 0 {
			w.maxEntries = limit
		}
	}
}

func WithMaxReadBytes(limit int64) Option {
	return func(w *Workspace) {
		if limit > 0 {
			w.maxReadBytes = limit
		}
	}
}

func WithMaxMatches(limit int) Option {
	return func(w *Workspace) {
		if limit > 0 {
			w.maxMatches = limit
		}
	}
}

func WithMaxOutputBytes(limit int64) Option {
	return func(w *Workspace) {
		if limit > 0 {
			w.maxOutputBytes = limit
		}
	}
}

func New(fsys fs.FS, options ...Option) *Workspace {
	w := &Workspace{
		fsys:           fsys,
		maxEntries:     DefaultMaxEntries,
		maxReadBytes:   DefaultMaxReadBytes,
		maxMatches:     DefaultMaxMatches,
		maxLineBytes:   DefaultMaxLineBytes,
		maxOutputBytes: DefaultMaxOutputBytes,
	}
	for _, option := range options {
		option(w)
	}
	return w
}

func NewSnapshot(ctx context.Context, sourceDir string, options ...Option) (*Workspace, string, error) {
	info, err := os.Stat(sourceDir)
	if err != nil {
		return nil, "", fmt.Errorf("stat source: %w", err)
	}
	if !info.IsDir() {
		return nil, "", fmt.Errorf("source %q: %w", sourceDir, ErrNotDir)
	}

	root, err := os.MkdirTemp("", "agentlab-sandbox-*")
	if err != nil {
		return nil, "", fmt.Errorf("create sandbox: %w", err)
	}
	if err := copyTree(ctx, sourceDir, root); err != nil {
		_ = os.RemoveAll(root)
		return nil, "", err
	}

	return New(os.DirFS(root), options...), root, nil
}

type ListRequest struct {
	Path       string
	Recursive  bool
	MaxEntries int
}

type ListResult struct {
	Entries   []Entry
	Truncated bool
}

type Entry struct {
	Path string
	Type EntryType
	Size int64
	Mode fs.FileMode
}

type EntryType string

const (
	EntryTypeFile EntryType = "file"
	EntryTypeDir  EntryType = "dir"
)

type ReadRequest struct {
	Path      string
	StartLine int
	LineCount int
	MaxBytes  int64
}

type ReadResult struct {
	Path       string
	Text       string
	StartLine  int
	EndLine    int
	Truncated  bool
	TotalBytes int64
}

type GrepRequest struct {
	Path          string
	Query         string
	CaseSensitive bool
	Regex         bool
	MaxMatches    int
}

type GrepResult struct {
	Matches   []Match
	Truncated bool
}

type Match struct {
	Path   string
	Line   int
	Text   string
	Column int
}

func (w *Workspace) List(ctx context.Context, req ListRequest) (ListResult, error) {
	root, err := cleanSandboxPath(req.Path)
	if err != nil {
		return ListResult{}, err
	}
	limit := firstPositive(req.MaxEntries, w.maxEntries)

	info, err := fs.Stat(w.fsys, root)
	if err != nil {
		return ListResult{}, err
	}
	if !info.IsDir() {
		return ListResult{}, fmt.Errorf("%s: %w", root, ErrNotDir)
	}

	var entries []Entry
	addEntry := func(p string, d fs.DirEntry) error {
		if len(entries) >= limit {
			return ErrTooLarge
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s: symlinks are not allowed", p)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entryType := EntryTypeFile
		if info.IsDir() {
			entryType = EntryTypeDir
		}
		entries = append(entries, Entry{
			Path: displayPath(p),
			Type: entryType,
			Size: info.Size(),
			Mode: info.Mode(),
		})
		return nil
	}

	truncated := false
	if req.Recursive {
		err = fs.WalkDir(w.fsys, root, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if p == root {
				return nil
			}
			if err := addEntry(p, d); err != nil {
				if errors.Is(err, ErrTooLarge) {
					truncated = true
					return fs.SkipAll
				}
				return err
			}
			return nil
		})
	} else {
		dirEntries, readErr := fs.ReadDir(w.fsys, root)
		if readErr != nil {
			return ListResult{}, readErr
		}
		for _, d := range dirEntries {
			if err := ctx.Err(); err != nil {
				return ListResult{}, err
			}
			if err := addEntry(joinSandbox(root, d.Name()), d); err != nil {
				if errors.Is(err, ErrTooLarge) {
					truncated = true
					break
				}
				return ListResult{}, err
			}
		}
	}
	if err != nil {
		return ListResult{}, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return ListResult{Entries: entries, Truncated: truncated}, nil
}

func (w *Workspace) Read(ctx context.Context, req ReadRequest) (ReadResult, error) {
	p, err := cleanSandboxPath(req.Path)
	if err != nil {
		return ReadResult{}, err
	}
	info, err := fs.Stat(w.fsys, p)
	if err != nil {
		return ReadResult{}, err
	}
	if !info.Mode().IsRegular() {
		return ReadResult{}, fmt.Errorf("%s: %w", p, ErrNotFile)
	}

	maxBytes := req.MaxBytes
	if maxBytes <= 0 {
		maxBytes = w.maxReadBytes
	}
	file, err := w.fsys.Open(p)
	if err != nil {
		return ReadResult{}, err
	}
	defer file.Close()

	limited := io.LimitReader(file, maxBytes+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return ReadResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return ReadResult{}, err
	}
	truncated := int64(len(data)) > maxBytes
	if truncated {
		data = data[:maxBytes]
	}
	if isBinary(data) {
		return ReadResult{}, fmt.Errorf("%s: %w", p, ErrBinaryFile)
	}

	startLine := req.StartLine
	if startLine <= 0 {
		startLine = 1
	}
	lines := splitLines(string(data))
	if startLine > len(lines) {
		return ReadResult{
			Path:       displayPath(p),
			StartLine:  startLine,
			EndLine:    startLine - 1,
			Truncated:  truncated,
			TotalBytes: info.Size(),
		}, nil
	}

	endExclusive := len(lines)
	if req.LineCount > 0 && startLine-1+req.LineCount < endExclusive {
		endExclusive = startLine - 1 + req.LineCount
		truncated = true
	}
	selected := lines[startLine-1 : endExclusive]

	return ReadResult{
		Path:       displayPath(p),
		Text:       strings.Join(selected, "\n"),
		StartLine:  startLine,
		EndLine:    endExclusive,
		Truncated:  truncated,
		TotalBytes: info.Size(),
	}, nil
}

func (w *Workspace) Grep(ctx context.Context, req GrepRequest) (GrepResult, error) {
	root, err := cleanSandboxPath(req.Path)
	if err != nil {
		return GrepResult{}, err
	}
	if req.Query == "" {
		return GrepResult{}, fmt.Errorf("query is required")
	}
	limit := firstPositive(req.MaxMatches, w.maxMatches)
	matcher, err := newMatcher(req.Query, req.CaseSensitive, req.Regex)
	if err != nil {
		return GrepResult{}, err
	}

	var files []string
	info, err := fs.Stat(w.fsys, root)
	if err != nil {
		return GrepResult{}, err
	}
	if info.Mode().IsRegular() {
		files = append(files, root)
	} else if info.IsDir() {
		err = fs.WalkDir(w.fsys, root, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf("%s: symlinks are not allowed", p)
			}
			if d.Type().IsRegular() {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return GrepResult{}, err
		}
	} else {
		return GrepResult{}, fmt.Errorf("%s: %w", root, ErrNotFile)
	}
	sort.Strings(files)

	matches := make([]Match, 0)
	outputBytes := int64(0)
	truncated := false
	for _, filePath := range files {
		if err := ctx.Err(); err != nil {
			return GrepResult{}, err
		}
		fileMatches, bytesMatched, err := w.grepFile(filePath, matcher, limit-len(matches), w.maxOutputBytes-outputBytes)
		outputBytes += bytesMatched
		matches = append(matches, fileMatches...)
		if err != nil {
			if errors.Is(err, ErrBinaryFile) {
				continue
			}
			if errors.Is(err, ErrTooLarge) {
				truncated = true
				break
			}
			return GrepResult{}, err
		}
		if len(matches) >= limit {
			truncated = true
			break
		}
	}
	return GrepResult{Matches: matches, Truncated: truncated}, nil
}

func (w *Workspace) grepFile(filePath string, matcher textMatcher, remaining int, remainingBytes int64) ([]Match, int64, error) {
	if remaining <= 0 || remainingBytes <= 0 {
		return nil, 0, ErrTooLarge
	}
	file, err := w.fsys.Open(filePath)
	if err != nil {
		return nil, 0, err
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	matches := make([]Match, 0)
	var lineNumber int
	var readBytes int64
	var outputBytes int64
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			readBytes += int64(len(line))
			if readBytes > w.maxReadBytes {
				return matches, outputBytes, ErrTooLarge
			}
			if len(line) > w.maxLineBytes {
				return matches, outputBytes, nil
			}
			if isBinary(line) {
				return nil, outputBytes, ErrBinaryFile
			}
			lineNumber++
			text := strings.TrimRight(string(line), "\r\n")
			if column, ok := matcher.Match(text); ok {
				if outputBytes+int64(len(text)) > remainingBytes {
					return matches, outputBytes, ErrTooLarge
				}
				outputBytes += int64(len(text))
				matches = append(matches, Match{
					Path:   displayPath(filePath),
					Line:   lineNumber,
					Text:   text,
					Column: column,
				})
				if len(matches) >= remaining {
					return matches, outputBytes, ErrTooLarge
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return matches, outputBytes, err
		}
	}
	return matches, outputBytes, nil
}

type textMatcher interface {
	Match(text string) (int, bool)
}

type literalMatcher struct {
	query         string
	caseSensitive bool
}

func (m literalMatcher) Match(text string) (int, bool) {
	haystack := text
	needle := m.query
	if !m.caseSensitive {
		haystack = strings.ToLower(haystack)
		needle = strings.ToLower(needle)
	}
	index := strings.Index(haystack, needle)
	if index < 0 {
		return 0, false
	}
	return index + 1, true
}

type regexMatcher struct {
	re *regexp.Regexp
}

func (m regexMatcher) Match(text string) (int, bool) {
	indexes := m.re.FindStringIndex(text)
	if indexes == nil {
		return 0, false
	}
	return indexes[0] + 1, true
}

func newMatcher(query string, caseSensitive, regex bool) (textMatcher, error) {
	if !regex {
		return literalMatcher{query: query, caseSensitive: caseSensitive}, nil
	}
	pattern := query
	if !caseSensitive {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return regexMatcher{re: re}, nil
}

func cleanSandboxPath(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "." {
		return ".", nil
	}
	if filepath.IsAbs(raw) || path.IsAbs(raw) || filepath.VolumeName(raw) != "" {
		return "", fmt.Errorf("%q: %w", raw, ErrInvalidPath)
	}
	normalized := path.Clean(strings.ReplaceAll(raw, "\\", "/"))
	if normalized == "." {
		return ".", nil
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("%q: %w", raw, ErrInvalidPath)
		}
	}
	return normalized, nil
}

func joinSandbox(root, name string) string {
	if root == "." {
		return name
	}
	return path.Join(root, name)
}

func displayPath(p string) string {
	if p == "." {
		return "."
	}
	return strings.TrimPrefix(p, "./")
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 1
}

func splitLines(text string) []string {
	text = strings.TrimSuffix(text, "\n")
	if text == "" {
		return nil
	}
	return strings.Split(text, "\n")
}

func isBinary(data []byte) bool {
	return bytes.IndexByte(data, 0) >= 0
}

func copyTree(ctx context.Context, sourceRoot, destRoot string) error {
	sourceRoot, err := filepath.Abs(sourceRoot)
	if err != nil {
		return fmt.Errorf("resolve source: %w", err)
	}
	return filepath.WalkDir(sourceRoot, func(sourcePath string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		rel, err := filepath.Rel(sourceRoot, sourcePath)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: symlinks are not allowed", rel)
		}
		destPath := filepath.Join(destRoot, rel)
		if info.IsDir() {
			return os.MkdirAll(destPath, info.Mode().Perm())
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(destPath), 0o700); err != nil {
			return err
		}
		return copyFile(sourcePath, destPath, info.Mode().Perm())
	})
}

func copyFile(sourcePath, destPath string, perm fs.FileMode) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	dest, err := os.OpenFile(destPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	defer dest.Close()

	if _, err := io.Copy(dest, source); err != nil {
		return err
	}
	return dest.Close()
}
