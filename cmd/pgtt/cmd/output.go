package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// outputFormat is a validated --output value.
type outputFormat string

const (
	outputTable outputFormat = "table"
	outputJSON  outputFormat = "json"
)

// parseOutputFormat validates the --output flag value (REQ-015).
func parseOutputFormat(s string) (outputFormat, error) {
	switch outputFormat(strings.ToLower(s)) {
	case outputTable:
		return outputTable, nil
	case outputJSON:
		return outputJSON, nil
	default:
		return "", fmt.Errorf("invalid --output %q: expected %q or %q", s, outputTable, outputJSON)
	}
}

// renderJSON writes v as indented JSON.
func renderJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// render writes data either as JSON (the whole value) or as a text table built
// from headers + rows, according to the global --output flag (REQ-015 / AC-007).
// The caller supplies both representations so each command controls its columns.
func render(w io.Writer, data any, headers []string, rows [][]string) error {
	format, err := parseOutputFormat(opts.output)
	if err != nil {
		return err
	}
	if format == outputJSON {
		return renderJSON(w, data)
	}
	return renderTable(w, headers, rows)
}

// renderTable writes rows as an aligned text table with the given headers.
// rows[i] must have the same length as headers.
func renderTable(w io.Writer, headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if len(headers) > 0 {
		if _, err := fmt.Fprintln(tw, strings.Join(headers, "\t")); err != nil {
			return err
		}
	}
	for _, r := range rows {
		if _, err := fmt.Fprintln(tw, strings.Join(r, "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}
