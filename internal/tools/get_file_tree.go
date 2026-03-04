package tools

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type GetFileTreeArgs struct {
	Repo       string `json:"repo" jsonschema:"Repository name"`
	PathPrefix string `json:"path_prefix,omitempty" jsonschema:"Filter by path prefix"`
}

type TreeNode struct {
	Type        string     `json:"type"` // "dir" or "file"
	Path        string     `json:"path"`
	Name        string     `json:"name"`
	Language    string     `json:"language,omitempty"`
	SymbolCount int        `json:"symbol_count,omitempty"`
	Children    []TreeNode `json:"children,omitempty"`
}

func GetFileTreeHandler(deps *Deps) func(context.Context, *mcp.CallToolRequest, GetFileTreeArgs) (*mcp.CallToolResult, any, error) {
	return func(ctx context.Context, req *mcp.CallToolRequest, args GetFileTreeArgs) (*mcp.CallToolResult, any, error) {
		t := newTimer()

		repoID, err := deps.Store.GetRepoID(args.Repo)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		files, err := deps.Store.GetFiles(repoID)
		if err != nil {
			r, _ := errorResult(err.Error())
			return r, nil, nil
		}

		// Count symbols per file
		allSymbols, _ := deps.Store.GetAllSymbols(repoID)
		symCounts := make(map[string]int)
		for _, s := range allSymbols {
			symCounts[s.File]++
		}

		// Build tree
		root := &TreeNode{Type: "dir", Path: "/", Name: "/"}
		for _, f := range files {
			if args.PathPrefix != "" && !strings.HasPrefix(f.Path, args.PathPrefix) {
				continue
			}
			addToTree(root, f.Path, f.Language, symCounts[f.Path])
		}

		result := map[string]any{
			"repo": args.Repo,
			"tree": root.Children,
			"_meta": Meta{
				TimingMs:  t.elapsedMs(),
				FileCount: len(files),
			},
		}
		r, _ := toTextResult(result)
		return r, nil, nil
	}
}

func addToTree(root *TreeNode, path, language string, symbolCount int) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	current := root
	for i, part := range parts {
		isFile := i == len(parts)-1

		if isFile {
			current.Children = append(current.Children, TreeNode{
				Type:        "file",
				Path:        path,
				Name:        part,
				Language:    language,
				SymbolCount: symbolCount,
			})
		} else {
			// Find or create dir
			found := false
			for j := range current.Children {
				if current.Children[j].Type == "dir" && current.Children[j].Name == part {
					current = &current.Children[j]
					found = true
					break
				}
			}
			if !found {
				current.Children = append(current.Children, TreeNode{
					Type: "dir",
					Name: part,
					Path: strings.Join(parts[:i+1], "/"),
				})
				current = &current.Children[len(current.Children)-1]
			}
		}
	}
}
