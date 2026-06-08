package rag

import (
	"context"
	"fmt"

	"github.com/formonkey/moa/tool/functiontool"
	"github.com/formonkey/moa/tool"
)

// SearchDocsArgs is the input schema for the search_docs tool.
type SearchDocsArgs struct {
	Query string `json:"query" jsonschema:"description=Keyword or topic to search in the project documentation (e.g. store routing forms guards signals naming alias),required"`
}

// SearchDocsResult is the output schema for the search_docs tool.
type SearchDocsResult struct {
	Sections []string `json:"sections"`
	Query    string   `json:"query"`
	Total    int      `json:"total"`
}

// NewSearchDocsTool creates a search_docs tool backed by the given RAG store.
func NewSearchDocsTool(store *Store) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "search_docs",
		Description: "Search the project's documentation by keyword. Returns matching sections with their source file. Use this BEFORE writing code to find the correct patterns, naming conventions, and architecture rules.",
	}, func(ctx context.Context, args SearchDocsArgs) (SearchDocsResult, error) {
		results, err := store.Search(args.Query, 10)
		if err != nil {
			return SearchDocsResult{}, fmt.Errorf("search_docs: %w", err)
		}

		sections := make([]string, 0, len(results))
		for _, sec := range results {
			sections = append(sections, fmt.Sprintf("[%s] %s", sec.File, sec.Content))
		}

		return SearchDocsResult{
			Sections: sections,
			Query:    args.Query,
			Total:    len(sections),
		}, nil
	})
}
