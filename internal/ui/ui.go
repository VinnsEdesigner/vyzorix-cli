
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// ANSI SGR sequences. Kept here so the rest of the CLI never writes an escape.
const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	red    = "\033[31m"
	green  = "\033[32m"
	yellow = "\033[33m"
	cyan   = "\033[36m"
)

// Printer writes the CLI's human-facing output to one stream.
type Printer struct {
	out   io.Writer
	color bool
}
.
func New(w io.Writer) *Printer {
	return &Printer{out: w, color: colorEnabled(w)}
}

// colorEnabled decides whether to emit ANSI escapes for w. NO_COLOR is checked
// first: it is the explicit opt-out, so it wins over FORCE_COLOR.
func colorEnabled(w io.Writer) bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return false
	}

	if v := os.Getenv("FORCE_COLOR"); v != "" && v != "0" {
		return true
	}

	if os.Getenv("TERM") == "dumb" {
		return false
	}

	f, ok := w.(*os.File)
	if !ok {
		return false
	}

	info, err := f.Stat()
	if err != nil {
		return false
	}

	return info.Mode()&os.ModeCharDevice != 0
}

// paint wraps s in a sequence when colour is on, and returns it unchanged when
// it is not, so a caller never has to branch.
func (p *Printer) paint(seq, s string) string {
	if !p.color {
		return s
	}

	return seq + s + reset
}

// Step prints a top-level action: "==> Deploying to d_01H". The marker is the
// bold, coloured anchor a reader scans for.
func (p *Printer) Step(format string, a ...any) {
	fmt.Fprintf(p.out, "%s %s\n", p.paint(bold+cyan, "==>"), fmt.Sprintf(format, a...))
}

// Success prints a completed action: "✓ Deployed version 3".
func (p *Printer) Success(format string, a ...any) {
	fmt.Fprintf(p.out, "%s %s\n", p.paint(green, "✓"), fmt.Sprintf(format, a...))
}

// Warn prints a non-fatal note: "! ...".
func (p *Printer) Warn(format string, a ...any) {
	fmt.Fprintf(p.out, "%s %s\n", p.paint(yellow, "!"), fmt.Sprintf(format, a...))
}

// Error prints a failure: "✗ ...".
func (p *Printer) Error(format string, a ...any) {
	fmt.Fprintf(p.out, "%s %s\n", p.paint(red, "✗"), fmt.Sprintf(format, a...))
}

// Detail prints an indented, dimmed line under a step or a result.
func (p *Printer) Detail(format string, a ...any) {
	fmt.Fprintf(p.out, "    %s\n", p.paint(dim, fmt.Sprintf(format, a...)))
}

// Fields prints aligned "key  value" rows under a two-space indent. The key
// column is as wide as the longest key, so the values line up.
func (p *Printer) Fields(pairs [][2]string) {
	width := 0
	for _, kv := range pairs {
		if len(kv[0]) > width {
			width = len(kv[0])
		}
	}

	for _, kv := range pairs {
		fmt.Fprintf(p.out, "  %s  %s\n", p.paint(dim, pad(kv[0], width)), kv[1])
	}
}

// Table prints a header row and the rows beneath it, each column padded to the
// widest cell so the columns align. The header is dimmed.
func (p *Printer) Table(headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}

	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	fmt.Fprintf(p.out, "  %s\n", p.paint(dim, joinPadded(headers, widths)))

	for _, row := range rows {
		fmt.Fprintf(p.out, "  %s\n", joinPadded(row, widths))
	}
}

// Blank prints one empty line, so a section can be separated without a literal
// escape in the caller.
func (p *Printer) Blank() {
	fmt.Fprintln(p.out)
}

// joinPadded pads every cell but the last, so a trailing space never appears.
func joinPadded(cells []string, widths []int) string {
	var b strings.Builder

	for i, cell := range cells {
		if i > 0 {
			b.WriteString("  ")
		}

		if i < len(cells)-1 {
			b.WriteString(pad(cell, widths[i]))
		} else {
			b.WriteString(cell)
		}
	}

	return b.String()
}

// pad right-pads s to width. A cell already at or past width is returned as is.
func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}

	return s + strings.Repeat(" ", width-len(s))
}
