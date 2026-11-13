package deploy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	vyzorixspec "github.com/VinnsEdesigner/vyzorix-spec"
	"github.com/pelletier/go-toml/v2"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// SchemaVersion is the manifest schema the CLI pins. It is the {version} of
// GET /v1/deploy/schema/{version} (FR-2). Pinning it means a manifest this CLI
// accepted is the manifest the server accepts: if the server has moved on, the
// fetch is a 404 and the CLI refuses rather than validating against a shape the
// server no longer uses.
const SchemaVersion = vyzorixspec.SchemaVersion

// Manifest is a parsed vyzorix.toml: the decoded document the schema validated,
// plus the shared wire types for display. The typed view comes from the spec
// module (FR-23), so the CLI never re-declares the manifest shape.
type Manifest struct {
	// Raw is the file's bytes, uploaded unchanged. The server parses the TOML
	// itself and is authoritative (FR-1), so the CLI never re-serialises it.
	Raw []byte
	// Typed is the decoded manifest, for the pre-upload summary.
	Typed vyzorixspec.DeployManifest
	// Doc is the TOML decoded into the JSON shape the schema describes, so it can
	// be validated and displayed through one representation.
	Doc map[string]any
}

// LoadManifest reads and decodes a manifest. It does not validate against the
// schema; that needs the server's published schema, which Validate does.
func LoadManifest(data []byte) (*Manifest, error) {
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("vyzorix.toml is not valid TOML: %w", err)
	}

	// The decoded TOML already has the schema's JSON shape (string keys, arrays,
	// scalars), so one JSON round-trip yields the shared typed view. This is why
	// the CLI can use the generated types without a second TOML struct: the schema
	// is the contract and both sides read the same JSON shape.
	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("could not read the manifest: %w", err)
	}

	var typed vyzorixspec.DeployManifest
	if err := json.Unmarshal(encoded, &typed); err != nil {
		return nil, fmt.Errorf("could not read the manifest: %w", err)
	}

	return &Manifest{Raw: data, Typed: typed, Doc: doc}, nil
}

// SchemaURL is where the manifest schema is served for this client's server.
// It is the key the fetched document is validated under, so the fetch and the
// validation cannot disagree about which URL the schema came from.
func (c *Client) SchemaURL() string {
	return c.baseURL + "/v1/deploy/schema/" + SchemaVersion
}

// Schema fetches the published manifest schema from the server.
func (c *Client) Schema(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.SchemaURL(), nil)
	if err != nil {
		return nil, err
	}

	// The schema is public: no auth header is sent, so this call works before the
	// CLI holds any credential.
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, decodeAPIError(resp.StatusCode, payload)
	}

	return payload, nil
}

// Validate checks a manifest against the published schema. It fails before
// upload, which is the point: a schema error is caught by the CLI with a
// readable field path rather than by the server as a refusal.
//
// schemaURL is where the schema was fetched from - the server's own URL, not a
// baked-in domain. The document carries no $id and every $ref in it is
// fragment-only, so keying it by that URL is enough to resolve it and a
// self-hosted server needs no special case.
func Validate(schemaURL string, schema []byte, manifest *Manifest) error {
	return validateDocument(schemaURL, schema, manifest.Doc, "vyzorix.toml")
}

// validateDocument compiles a JSON Schema and validates one decoded document
// against it. Shared by the manifest and recipe validators.
func validateDocument(schemaURL string, schema []byte, doc map[string]any, what string) error {
	compiled, err := compileSchema(schemaURL, schema)
	if err != nil {
		return err
	}

	if err := compiled.Validate(doc); err != nil {
		return fmt.Errorf("%s does not match the server's schema: %w", what, err)
	}

	return nil
}

// compileSchema keys a fetched schema by the URL it came from and compiles it.
func compileSchema(schemaURL string, schema []byte) (*jsonschema.Schema, error) {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(schema))
	if err != nil {
		return nil, fmt.Errorf("the server's schema could not be read: %w", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(schemaURL, doc); err != nil {
		return nil, fmt.Errorf("the server's schema could not be read: %w", err)
	}

	compiled, err := compiler.Compile(schemaURL)
	if err != nil {
		return nil, fmt.Errorf("the server's schema could not be compiled: %w", err)
	}

	return compiled, nil
}
