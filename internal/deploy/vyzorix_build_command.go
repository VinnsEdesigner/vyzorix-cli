package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	vyzorixspec "github.com/VinnsEdesigner/vyzorix-spec"
	"github.com/pelletier/go-toml/v2"

	"github.com/VinnsEdesigner/vyzorix-cli/internal/ui"
)

// BuildSchemaVersion is the recipe schema this CLI pins.
const BuildSchemaVersion = vyzorixspec.BuildSchemaVersion

// Recipe is a parsed vyzorix.build.toml.
type Recipe struct {
	Raw   []byte
	Typed vyzorixspec.BuildRecipe
	Doc   map[string]any
}

// BuildOptions is one `vyzorix build` invocation.
type BuildOptions struct {
	Version      string
	Device       string
	Org          string
	ManifestPath string
	ProjectDir   string
	Publish      Publish
	Limits       *Limits
	DryRun       bool
}

// BuildOutcome is what `vyzorix build` produced. A dry run fills only Artifacts
// (with no url or digest) and sets DryRun.
type BuildOutcome struct {
	Artifacts    []BuiltArtifact
	Manifest     []byte
	ManifestPath string
	DryRun       bool
}

// LoadRecipe reads and decodes a vyzorix.build.toml. Validation needs the
// server's published schema, which ValidateRecipe does.
func LoadRecipe(data []byte) (*Recipe, error) {
	var doc map[string]any
	if err := toml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("vyzorix.build.toml is not valid TOML: %w", err)
	}

	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("could not read the recipe: %w", err)
	}

	var typed vyzorixspec.BuildRecipe
	if err := json.Unmarshal(encoded, &typed); err != nil {
		return nil, fmt.Errorf("could not read the recipe: %w", err)
	}

	return &Recipe{Raw: data, Typed: typed, Doc: doc}, nil
}

// BuildSchemaURL is where the recipe schema is served for this client's server.
func (c *Client) BuildSchemaURL() string {
	return c.baseURL + "/v1/deploy/build-schema/" + BuildSchemaVersion
}

// BuildSchema fetches the published recipe schema. It is public, like the
// manifest schema.
func (c *Client) BuildSchema(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BuildSchemaURL(), nil)
	if err != nil {
		return nil, err
	}

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

// ValidateRecipe checks a recipe against the published recipe schema.
func ValidateRecipe(schemaURL string, schema []byte, recipe *Recipe) error {
	return validateDocument(schemaURL, schema, recipe.Doc, "vyzorix.build.toml")
}

// Build loads the recipe, resolves the target ABI from the server, validates
// against the published schema, delegates to vyzorix-builder, and renders the
// manifest from what the builder produced. The CLI compiles nothing itself.
func (c *Client) Build(ctx context.Context, recipeData []byte, opts BuildOptions, p *ui.Printer) (*BuildOutcome, error) {
	recipe, err := LoadRecipe(recipeData)
	if err != nil {
		return nil, err
	}

	schema, err := c.BuildSchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not fetch the build-recipe schema: %w", err)
	}

	if err := ValidateRecipe(c.BuildSchemaURL(), schema, recipe); err != nil {
		return nil, err
	}

	abis, err := c.resolveABIs(ctx, recipe, opts)
	if err != nil {
		return nil, err
	}

	bin, err := builderPath()
	if err != nil {
		return nil, err
	}

	projectDir, err := absProjectDir(opts.ProjectDir)
	if err != nil {
		return nil, err
	}

	version := opts.Version
	if version == "" {
		version = recipe.Typed.Build.Version
	}

	request := BuildRequest{
		Recipe:     string(recipe.Raw),
		SchemaURL:  c.BuildSchemaURL(),
		ProjectDir: projectDir,
		Version:    version,
		DeviceID:   opts.Device,
		ABIs:       abis,
		Publish:    opts.Publish,
		DryRun:     opts.DryRun,
	}

	printBuildPlan(p, recipe, abis, opts)

	result, err := runBuilder(ctx, bin, request)
	if err != nil {
		return nil, err
	}

	// A dry run publishes nothing, so the artifacts carry no url or digest and a
	// manifest rendered from them would be invalid. Report the plan and stop,
	// rather than overwrite the operator's manifest with placeholders.
	if opts.DryRun {
		return &BuildOutcome{Artifacts: result.Artifacts, DryRun: true}, nil
	}

	// A dry run publishes nothing, so the artifacts carry no url or digest and a
	// manifest rendered from them would be invalid. Report the plan and stop,
	// rather than overwrite the operator's manifest with placeholders.
	if opts.DryRun {
		return &BuildOutcome{Artifacts: result.Artifacts, DryRun: true}, nil
	}

	manifest, err := RenderManifest(&recipe.Typed, result.Artifacts, ManifestTarget{
		DeviceID: opts.Device,
		Org:      opts.Org,
	})
	if err != nil {
		return nil, err
	}

	path := opts.ManifestPath
	if path == "" {
		path = "vyzorix.toml"
	}

	if err := writeFile(path, manifest); err != nil {
		return nil, err
	}

	return &BuildOutcome{Artifacts: result.Artifacts, Manifest: manifest, ManifestPath: path}, nil
}

// resolveABIs is the target set the builder must produce: the recipe's `abi`, or
// the device's own ABIs, read from the server so an operator never guesses.
//
// When the recipe sets `abi` AND a device is named, the two are checked against
// each other: building for an ABI the device does not run is refused here rather
// than discovered when the device cannot start the artifact (contract R21).
func (c *Client) resolveABIs(ctx context.Context, recipe *Recipe, opts BuildOptions) ([]string, error) {
	if len(recipe.Typed.Build.Abi) > 0 {
		if opts.Device == "" {
			return recipe.Typed.Build.Abi, nil
		}

		identity, err := c.Whoami(ctx, opts.Device)
		if err != nil {
			return nil, err
		}

		// A device that reports no ABIs cannot be checked; the recipe stands.
		if identity.Device == nil || len(identity.Device.SupportedABIs) == 0 {
			return recipe.Typed.Build.Abi, nil
		}

		if missing := abisNotSupported(recipe.Typed.Build.Abi, identity.Device.SupportedABIs); len(missing) > 0 {
			return nil, fmt.Errorf(
				"the recipe targets %v but device %q runs %v; building for %v would produce an artifact the device cannot start",
				recipe.Typed.Build.Abi, opts.Device, identity.Device.SupportedABIs, missing)
		}

		return recipe.Typed.Build.Abi, nil
	}

	if opts.Device == "" {
		return nil, fmt.Errorf("the recipe sets no abi, so --device is required to resolve it from the device")
	}

	identity, err := c.Whoami(ctx, opts.Device)
	if err != nil {
		return nil, err
	}

	if identity.Device == nil || len(identity.Device.SupportedABIs) == 0 {
		return nil, fmt.Errorf("the server reported no ABIs for device %q; set `abi` in the recipe", opts.Device)
	}

	return identity.Device.SupportedABIs, nil
}

// abisNotSupported returns the ABIs in want that the device does not support. It
// is the check behind resolveABIs: an empty result means every wanted ABI is one
// the device runs.
func abisNotSupported(want, supported []string) []string {
	set := make(map[string]bool, len(supported))
	for _, a := range supported {
		set[a] = true
	}

	var missing []string
	for _, a := range want {
		if !set[a] {
			missing = append(missing, a)
		}
	}

	return missing
}

// printBuildPlan prints what will be built, before anything expensive happens.
func printBuildPlan(p *ui.Printer, recipe *Recipe, abis []string, opts BuildOptions) {
	p.Step("Building %d artifact(s) for %v", len(recipe.Typed.Artifact), abis)

	for _, art := range recipe.Typed.Artifact {
		p.Detail("  %s/%s  %s", art.App, art.Name, art.Lang)
	}

	if opts.DryRun {
		p.Detail("  dry run: nothing is published")
	}
}

// PrintBuildReport writes the outcome. A dry run reports the plan; a real build
// reports each artifact's digest and the manifest it wrote.
func PrintBuildReport(p *ui.Printer, out *BuildOutcome) {
	if out.DryRun {
		p.Success("Dry run: %d artifact(s) would be built", len(out.Artifacts))
		p.Detail("Nothing was published and no manifest was written.")

		return
	}

	p.Success("Built %d artifact(s)", len(out.Artifacts))

	// Include the tag column only when a registry returned one, so a local or
	// object backend does not print an empty column.
	hasTag := false
	for _, a := range out.Artifacts {
		if a.Tag != "" {
			hasTag = true
			break
		}
	}

	rows := make([][]string, 0, len(out.Artifacts))
	for _, a := range out.Artifacts {
		arch := a.Arch
		if arch == "" {
			arch = "any"
		}

		row := []string{a.Name, arch, shortDigest(a.SHA256)}
		if hasTag {
			row = append(row, a.Tag)
		}
		rows = append(rows, row)
	}

	p.Blank()
	if hasTag {
		p.Table([]string{"ARTIFACT", "ARCH", "SHA256", "TAG"}, rows)
	} else {
		p.Table([]string{"ARTIFACT", "ARCH", "SHA256"}, rows)
	}
	p.Blank()
	p.Detail("Wrote %s", out.ManifestPath)
}

// shortDigest is the first 12 hex characters of a digest.
func shortDigest(s string) string {
	const n = 12
	if len(s) <= n {
		return s
	}

	return s[:n]
}

// writeFile writes atomically, via a temp file and a rename.
func writeFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("could not create %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".vyzorix-*.tmp")
	if err != nil {
		return fmt.Errorf("could not write %s: %w", path, err)
	}

	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)

		return fmt.Errorf("could not write %s: %w", path, err)
	}

	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)

		return fmt.Errorf("could not write %s: %w", path, err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)

		return fmt.Errorf("could not write %s: %w", path, err)
	}

	return nil
}
