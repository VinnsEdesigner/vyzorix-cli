package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunBuilderDecodesResult writes a stub builder that speaks the documented
// JSON contract, points the delegate at it, and pins that the CLI reads the
// artifacts it produced. The stub is a real executable, not a mock: the process
// boundary is what is under test.
func TestRunBuilderDecodesResult(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "vyzorix-builder")

	script := `#!/bin/sh
read_request() { cat > /dev/null; }
read_request
cat <<'JSON'
{"artifacts":[{"name":"engine","arch":"arm64-v8a","kind":"dir","url":"https://ghcr.io/v2/o/r/blobs/sha256:` + shaEngine + `","sha256":"` + shaEngine + `","file":"vfd-engine-linux-android-arm64-v8a-v1.2.0.tar.gz"}]}
JSON
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	t.Setenv("VYZORIX_BUILDER", stub)

	result, err := runBuilder(context.Background(), stub, BuildRequest{
		Recipe:     "[build]\nversion = \"v1.2.0\"\n",
		ProjectDir: dir,
		ABIs:       []string{"arm64-v8a"},
	})
	if err != nil {
		t.Fatalf("runBuilder = %v", err)
	}

	if len(result.Artifacts) != 1 || result.Artifacts[0].Name != "engine" {
		t.Fatalf("artifacts = %+v, want one engine", result.Artifacts)
	}

	if result.Artifacts[0].SHA256 != shaEngine {
		t.Errorf("sha256 = %q, want %q", result.Artifacts[0].SHA256, shaEngine)
	}
}

// TestRunBuilderRefusalIsAOneLiner pins that a builder refusal (exit 1) surfaces
// as a BuilderError carrying the builder's first line, so the CLI prints the
// builder's reason rather than a stack of build output.
func TestRunBuilderRefusalIsAOneLiner(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "vyzorix-builder")

	script := `#!/bin/sh
cat > /dev/null
echo "engine: no artifact matched arm64-v8a" >&2
echo "second line of noise" >&2
exit 1
`
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	_, err := runBuilder(context.Background(), stub, BuildRequest{ProjectDir: dir})
	if err == nil {
		t.Fatal("runBuilder = nil, want a refusal")
	}

	var refusal *BuilderError
	if !asBuilderError(err, &refusal) {
		t.Fatalf("runBuilder = %T, want *BuilderError", err)
	}

	if refusal.Message != "engine: no artifact matched arm64-v8a" {
		t.Errorf("message = %q, want the builder's first line", refusal.Message)
	}
}

// TestRunBuilderEmptyResultRefuses proves a builder that returns no artifacts is
// a local failure, not a silent empty manifest.
func TestRunBuilderEmptyResultRefuses(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "vyzorix-builder")

	script := "#!/bin/sh\ncat > /dev/null\necho '{\"artifacts\":[]}'\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	_, err := runBuilder(context.Background(), stub, BuildRequest{ProjectDir: dir})
	if err == nil || !strings.Contains(err.Error(), "no artifacts") {
		t.Fatalf("runBuilder = %v, want a 'no artifacts' refusal", err)
	}
}

// TestBuilderPathPrefersEnv pins the resolution order: $VYZORIX_BUILDER first.
func TestBuilderPathPrefersEnv(t *testing.T) {
	t.Setenv("VYZORIX_BUILDER", "/opt/vyzorix-builder")
	if p, _ := builderPath(); p != "/opt/vyzorix-builder" {
		t.Errorf("builderPath = %q, want the env value", p)
	}
}

// TestBuilderPathMissingIsAHelpfulError pins that an absent builder names the
// remedy, rather than a bare exec error.
func TestBuilderPathMissingIsAHelpfulError(t *testing.T) {
	t.Setenv("VYZORIX_BUILDER", "")
	t.Setenv("PATH", t.TempDir())

	_, err := builderPath()
	if err == nil || !strings.Contains(err.Error(), "VYZORIX_BUILDER") {
		t.Fatalf("builderPath = %v, want an error naming VYZORIX_BUILDER", err)
	}
}

// asBuilderError is errors.As specialized to *BuilderError, kept local so the
// test needs no errors import for one assertion.
func asBuilderError(err error, target **BuilderError) bool {
	b, ok := err.(*BuilderError)
	if ok {
		*target = b
	}

	return ok
}
