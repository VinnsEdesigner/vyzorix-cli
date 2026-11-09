package main

import (
	"flag"
	"reflect"
	"testing"

	deploycli "github.com/VinnsEdesigner/vyzorix-cli/internal/deploy"
)

// TestParseInterspersed pins §9b.6: the documented `secrets set <app> KEY=VALUE
// --device <id>` puts the positionals before the flags, which the flag package
// alone would leave unparsed.
func TestParseInterspersed(t *testing.T) {
	cases := []struct {
		name   string
		args   []string
		want   []string
		device string
		org    string
	}{
		{"flags first", []string{"--device", "d1", "--org", "o1", "app", "K=V"}, []string{"app", "K=V"}, "d1", "o1"},
		{"positionals first", []string{"app", "K=V", "--device", "d1", "--org", "o1"}, []string{"app", "K=V"}, "d1", "o1"},
		{"interleaved", []string{"app", "--device", "d1", "K=V", "--org", "o1"}, []string{"app", "K=V"}, "d1", "o1"},
		{"flags only", []string{"--device", "d1", "--org", "o1"}, nil, "d1", "o1"},
		{"positionals only", []string{"app", "K=V"}, []string{"app", "K=V"}, "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fs := flag.NewFlagSet(tc.name, flag.ContinueOnError)
			device := fs.String("device", "", "")
			org := fs.String("org", "", "")

			got, err := parseInterspersed(fs, tc.args)
			if err != nil {
				t.Fatalf("parseInterspersed: %v", err)
			}

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("positionals = %q, want %q", got, tc.want)
			}

			// The flags must still be read, wherever they appeared.
			if *device != tc.device || *org != tc.org {
				t.Errorf("flags = device %q org %q, want %q/%q", *device, *org, tc.device, tc.org)
			}
		})
	}
}

// TestSplitAssignmentKeepsEqualsInValue pins that only the first '=' separates,
// so a URL or base64 value survives.
func TestSplitAssignmentKeepsEqualsInValue(t *testing.T) {
	key, value, err := deploycli.SplitAssignment("DATABASE_URL=postgres://u:p@h/db?a=b")
	if err != nil {
		t.Fatalf("SplitAssignment: %v", err)
	}

	if key != "DATABASE_URL" {
		t.Errorf("key = %q, want DATABASE_URL", key)
	}

	if value != "postgres://u:p@h/db?a=b" {
		t.Errorf("value = %q, want the whole value", value)
	}
}
