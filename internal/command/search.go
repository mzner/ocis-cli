package command

import (
	"fmt"
	"strings"
	"time"

	"github.com/mzner/ocis-cli/internal/app"
	"github.com/spf13/cobra"
)

func newSearchCommand(options *globalOptions) *cobra.Command {
	var (
		allSpaces      bool
		content        bool
		raw            bool
		remotePath     string
		resourceType   string
		mediaType      string
		minSize        string
		maxSize        string
		modifiedAfter  string
		modifiedBefore string
		limit          int
	)
	command := &cobra.Command{
		Use:     "search QUERY",
		Aliases: []string{"find"},
		Short:   "Search remote files and directories",
		Args:    exactArgs(1),
		Long: "Search indexed remote resources. Plain queries match names by substring; " +
			"use --raw for an oCIS KQL expression.",
		RunE: func(command *cobra.Command, args []string) error {
			if limit < 1 || limit > 1000 {
				return usageError("search", "--limit must be between 1 and 1000")
			}
			minimum, err := parseOptionalByteSize(minSize, "minimum size")
			if err != nil {
				return usageError("search", err.Error())
			}
			maximum, err := parseOptionalByteSize(maxSize, "maximum size")
			if err != nil {
				return usageError("search", err.Error())
			}
			after, err := parseOptionalSearchTime(modifiedAfter)
			if err != nil {
				return usageError("search", "--modified-after: "+err.Error())
			}
			before, err := parseOptionalSearchTime(modifiedBefore)
			if err != nil {
				return usageError("search", "--modified-before: "+err.Error())
			}
			return runSearch(command, options, app.SearchRequest{
				Query: args[0], Raw: raw, Content: content,
				AllSpaces: allSpaces, Path: remotePath,
				ResourceType: strings.ToLower(strings.TrimSpace(resourceType)),
				MediaType:    strings.TrimSpace(mediaType),
				MinSize:      minimum, MaxSize: maximum,
				ModifiedAfter: after, ModifiedBefore: before, Limit: limit,
			})
		},
	}
	command.Flags().BoolVar(
		&allSpaces, "all-spaces", false,
		"search every Space accessible to the authenticated user",
	)
	command.Flags().StringVar(
		&remotePath, "path", "", "limit search to a directory in the selected Space",
	)
	command.Flags().StringVar(
		&resourceType, "type", "", "resource type: file or directory",
	)
	command.Flags().StringVar(
		&mediaType, "media-type", "",
		"media category or MIME type, such as pdf, image, or application/pdf",
	)
	command.Flags().BoolVar(
		&content, "content", false,
		"search indexed file contents instead of names",
	)
	command.Flags().BoolVar(
		&raw, "raw", false, "treat QUERY as an oCIS KQL expression",
	)
	command.Flags().StringVar(
		&minSize, "min-size", "", "minimum size in bytes or KB, MB, GB, KiB, MiB, GiB",
	)
	command.Flags().StringVar(
		&maxSize, "max-size", "", "maximum size in bytes or KB, MB, GB, KiB, MiB, GiB",
	)
	command.Flags().StringVar(
		&modifiedAfter, "modified-after", "",
		"modified at or after YYYY-MM-DD or RFC3339",
	)
	command.Flags().StringVar(
		&modifiedBefore, "modified-before", "",
		"modified at or before YYYY-MM-DD or RFC3339",
	)
	command.Flags().IntVar(
		&limit, "limit", 100, "maximum number of results (1-1000)",
	)
	return command
}

func parseOptionalByteSize(value, label string) (*int64, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "default" || normalized == "unlimited" {
		return nil, fmt.Errorf("%s must be a finite byte size", label)
	}
	result, err := parseByteSize(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	return &result, nil
}

func parseOptionalSearchTime(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	for _, layout := range []string{time.RFC3339, time.DateOnly} {
		parsed, err := time.Parse(layout, value)
		if err == nil {
			return &parsed, nil
		}
	}
	return nil, fmt.Errorf("invalid time %q; use YYYY-MM-DD or RFC3339", value)
}
