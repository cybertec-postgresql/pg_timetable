package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOutputFormat(t *testing.T) {
	for _, tt := range []struct {
		in      string
		want    outputFormat
		wantErr bool
	}{
		{"table", outputTable, false},
		{"json", outputJSON, false},
		{"JSON", outputJSON, false},
		{"yaml", "", true},
		{"", "", true},
	} {
		got, err := parseOutputFormat(tt.in)
		if tt.wantErr {
			assert.Error(t, err, "input %q", tt.in)
			continue
		}
		require.NoError(t, err, "input %q", tt.in)
		assert.Equal(t, tt.want, got)
	}
}

func TestRenderJSON(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, renderJSON(&buf, map[string]int{"a": 1}))
	assert.Contains(t, buf.String(), `"a": 1`)
}

func TestRenderTable(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, renderTable(&buf, []string{"ID", "NAME"}, [][]string{{"1", "backup"}}))
	out := buf.String()
	assert.Contains(t, out, "ID")
	assert.Contains(t, out, "backup")
}

func TestReadYes(t *testing.T) {
	assert.True(t, readYes(strings.NewReader("y\n")))
	assert.True(t, readYes(strings.NewReader("YES\n")))
	assert.False(t, readYes(strings.NewReader("n\n")))
	assert.False(t, readYes(strings.NewReader("\n")))
}

// TestRender_JSONAndTable verifies the --output dispatcher (REQ-015 / AC-007).
func TestRender_JSONAndTable(t *testing.T) {
	t.Cleanup(func() { opts.output = "table" })
	data := []map[string]string{{"name": "backup"}}

	opts.output = "json"
	var jbuf bytes.Buffer
	require.NoError(t, render(&jbuf, data, nil, nil))
	assert.Contains(t, jbuf.String(), `"name": "backup"`)

	opts.output = "table"
	var tbuf bytes.Buffer
	require.NoError(t, render(&tbuf, data, []string{"NAME"}, [][]string{{"backup"}}))
	assert.Contains(t, tbuf.String(), "NAME")
	assert.Contains(t, tbuf.String(), "backup")

	opts.output = "bogus"
	require.Error(t, render(&tbuf, data, nil, nil))
}
