package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Publish names where the builder stores the artifacts.
type Publish struct {
	Backend    string `json:"backend"`
	Namespace  string `json:"namespace,omitempty"`
	Repo       string `json:"repo,omitempty"`
	Visibility string `json:"visibility,omitempty"`
}

// BuildRequest is what the CLI sends the builder over stdin. Recipe carries the
// recipe bytes unchanged; ABIs is the resolved target set, so the builder never
// guesses the device's architecture.
type BuildRequest struct {
	Recipe     string   `json:"recipe"`
	SchemaURL  string   `json:"schemaUrl"`
	ProjectDir string   `json:"projectDir"`
	Version    string   `json:"version,omitempty"`
	DeviceID   string   `json:"deviceId,omitempty"`
	ABIs       []string `json:"abis,omitempty"`
	Publish    Publish  `json:"publish"`
	DryRun     bool     `json:"dryRun,omitempty"`
}

// BuiltArtifact is one artifact the builder produced and published. URL and
// SHA256 are the pair the manifest references; File is the explicit filename.
type BuiltArtifact struct {
	Name   string `json:"name"`
	Arch   string `json:"arch,omitempty"`
	Kind   string `json:"kind,omitempty"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	File   string `json:"file,omitempty"`
	Tag    string `json:"tag,omitempty"`
	Size   int64  `json:"size,omitempty"`
}

// BuiltResult is the builder's reply on stdout.
type BuiltResult struct {
	Artifacts []BuiltArtifact `json:"artifacts"`
}

// BuilderError is a refusal from vyzorix-builder, carrying its message verbatim
// so the CLI prints it unchanged and exits 1, like a server refusal.
type BuilderError struct {
	Message string
}

// Error implements error.
func (e *BuilderError) Error() string { return e.Message }

// builderPath resolves the builder binary: $VYZORIX_BUILDER, else on PATH.
func builderPath() (string, error) {
	if p := os.Getenv("VYZORIX_BUILDER"); p != "" {
		return p, nil
	}

	p, err := exec.LookPath("vyzorix-builder")
	if err != nil {
		return "", errors.New("vyzorix-builder not found: install it, or set VYZORIX_BUILDER to its path")
	}

	return p, nil
}

// runBuilder invokes the builder with req and decodes its result. A non-zero exit
// comes back as a BuilderError carrying the builder's stderr.
func runBuilder(ctx context.Context, bin string, req BuildRequest) (*BuiltResult, error) {
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("could not encode the build request: %w", err)
	}

	cmd := exec.CommandContext(ctx, bin, "build")
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Dir = req.ProjectDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := stderr.String()
		if msg == "" {
			msg = err.Error()
		}

		return nil, &BuilderError{Message: firstLine(msg)}
	}

	var result BuiltResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return nil, fmt.Errorf("vyzorix-builder returned an unreadable result: %w", err)
	}

	if len(result.Artifacts) == 0 {
		return nil, errors.New("vyzorix-builder returned no artifacts")
	}

	return &result, nil
}

// firstLine keeps a multi-line builder diagnostic to its first line, so build
// output does not swamp the CLI's one-line error style.
func firstLine(s string) string {
	for i, r := range s {
		if r == '\n' {
			return s[:i]
		}
	}

	return s
}

// absProjectDir resolves the project directory the builder builds from. It must
// be absolute, because the builder runs with it as its working directory.
func absProjectDir(dir string) (string, error) {
	if dir == "" {
		dir = "."
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("could not resolve the project directory: %w", err)
	}

	return abs, nil
}
