package ui

import (
	"bytes"
	"strings"
	"testing"
)

// TestColorIsOffForNonTerminalWriters pins the property that keeps a CI log
// readable: a bytes.Buffer is not a terminal, so no escape sequence is written.
func TestColorIsOffForNonTerminalWriters(t *testing.T) {
	t.Setenv("FORCE_COLOR", "")

	var buf bytes.Buffer

	New(&buf).Success("done")

	if strings.Contains(buf.String(), "\033[") {
		t.Errorf("a non-terminal writer received an escape sequence: %q", buf.String())
	}
}

// TestForceColorOverridesTheTTYCheck pins the escape hatch: a CI that wants the
// styled output can ask for it, and that is also how the styled path is tested.
func TestForceColorOverridesTheTTYCheck(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")

	var buf bytes.Buffer

	New(&buf).Success("done")

	if !strings.Contains(buf.String(), "\033[") {
		t.Errorf("FORCE_COLOR=1 did not enable colour: %q", buf.String())
	}
}

// TestNoColorWins pins the standard opt-out: NO_COLOR disables colour even when
// FORCE_COLOR is set, because a user who set NO_COLOR meant it.
func TestNoColorWins(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	t.Setenv("NO_COLOR", "1")

	var buf bytes.Buffer

	New(&buf).Success("done")

	if strings.Contains(buf.String(), "\033[") {
		t.Errorf("NO_COLOR did not disable colour: %q", buf.String())
	}
}

// TestTableAlignsColumns pins the alignment: every column is as wide as its
// widest cell, so the values line up under the header.
func TestTableAlignsColumns(t *testing.T) {
	t.Setenv("FORCE_COLOR", "")

	var buf bytes.Buffer

	New(&buf).Table([]string{"KEY", "GENERATION"}, [][]string{
		{"DATABASE_URL", "1"},
		{"X", "12"},
	})

	want := "  KEY           GENERATION\n" +
		"  DATABASE_URL  1\n" +
		"  X             12\n"
	if buf.String() != want {
		t.Errorf("table =\n%q\nwant\n%q", buf.String(), want)
	}
}

// TestFieldsAlignValues pins the aligned key/value block.
func TestFieldsAlignValues(t *testing.T) {
	t.Setenv("FORCE_COLOR", "")

	var buf bytes.Buffer

	New(&buf).Fields([][2]string{
		{"authorised by", "op_01H"},
		{"artifacts", "1"},
	})

	want := "  authorised by  op_01H\n" +
		"  artifacts      1\n"
	if buf.String() != want {
		t.Errorf("fields =\n%q\nwant\n%q", buf.String(), want)
	}
}
