package agenttool

import (
	"context"
	"encoding/json"
	"fmt"

	"agentlab/internal/sandboxfs"
)

func NewSandboxRegistry(workspace *sandboxfs.Workspace) (*Registry, error) {
	return NewRegistry(SandboxDefinitions(workspace))
}

func SandboxDefinitions(workspace *sandboxfs.Workspace) []Definition {
	return []Definition{
		{
			Name:        "list_files",
			Description: "list files under a sandbox-relative path.",
			Parameters: Object(map[string]Schema{
				"path":        String("Sandbox-relative path to list. Use \".\" for the sandbox root."),
				"recursive":   Boolean("Whether to recursively list descendants."),
				"max_entries": Integer("Maximum number of entries to return."),
			}, "path"),
			Execute: func(ctx context.Context, args json.RawMessage) (any, error) {
				var request struct {
					Path       string `json:"path"`
					Recursive  bool   `json:"recursive"`
					MaxEntries int    `json:"max_entries"`
				}
				if err := json.Unmarshal(args, &request); err != nil {
					return nil, fmt.Errorf("parse list_files arguments: %w", err)
				}
				return workspace.List(ctx, sandboxfs.ListRequest{
					Path:       request.Path,
					Recursive:  request.Recursive,
					MaxEntries: request.MaxEntries,
				})
			},
		},
		{
			Name:        "read_file",
			Description: "read a sandbox-relative text file.",
			Parameters: Object(map[string]Schema{
				"path":       String("Sandbox-relative path to read."),
				"start_line": Integer("One-based line number to start reading from."),
				"line_count": Integer("Maximum number of lines to read."),
				"max_bytes":  Integer("Maximum number of bytes to read."),
			}, "path"),
			Execute: func(ctx context.Context, args json.RawMessage) (any, error) {
				var request struct {
					Path      string `json:"path"`
					StartLine int    `json:"start_line"`
					LineCount int    `json:"line_count"`
					MaxBytes  int64  `json:"max_bytes"`
				}
				if err := json.Unmarshal(args, &request); err != nil {
					return nil, fmt.Errorf("parse read_file arguments: %w", err)
				}
				return workspace.Read(ctx, sandboxfs.ReadRequest{
					Path:      request.Path,
					StartLine: request.StartLine,
					LineCount: request.LineCount,
					MaxBytes:  request.MaxBytes,
				})
			},
		},
		{
			Name:        "search_text",
			Description: "search sandbox text files.",
			Parameters: Object(map[string]Schema{
				"query":          String("Literal text or regular expression to search for."),
				"path":           String("Sandbox-relative path to search. Use \".\" for the sandbox root."),
				"case_sensitive": Boolean("Whether matching is case-sensitive."),
				"regex":          Boolean("Whether query is a regular expression."),
				"max_matches":    Integer("Maximum number of matches to return."),
			}, "query"),
			Execute: func(ctx context.Context, args json.RawMessage) (any, error) {
				var request struct {
					Path          string `json:"path"`
					Query         string `json:"query"`
					CaseSensitive bool   `json:"case_sensitive"`
					Regex         bool   `json:"regex"`
					MaxMatches    int    `json:"max_matches"`
				}
				if err := json.Unmarshal(args, &request); err != nil {
					return nil, fmt.Errorf("parse search_text arguments: %w", err)
				}
				return workspace.Grep(ctx, sandboxfs.GrepRequest{
					Path:          request.Path,
					Query:         request.Query,
					CaseSensitive: request.CaseSensitive,
					Regex:         request.Regex,
					MaxMatches:    request.MaxMatches,
				})
			},
		},
	}
}
