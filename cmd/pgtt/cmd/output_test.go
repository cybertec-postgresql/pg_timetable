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
