package deploy

import (
	"encoding/json"
	"strings"
	"testing"

	vyzorixspec "github.com/VinnsEdesigner/vyzorix-spec"
	"github.com/pelletier/go-toml/v2"
)

const (
	shaEngine  = "9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c9f2c"
	shaConsole = "41ab41ab41ab41ab41ab41ab41ab41ab41ab41ab41ab41ab41ab41ab41ab41ab"
)

// TestRenderManifestGroupsAndPinsDigests pins the renderer's two jobs: artifacts
// that share an app become one [[app]] block, and every digest/url comes from the
// build - never a value the recipe could carry.
func TestRenderManifestGroupsAndPinsDigests(t *testing.T) {
	recipe := &vyzorixspec.BuildRecipe{
		Build: vyzorixspec.BuildSettings{Version: "v1.2.0"},
		Artifact: []vyzorixspec.BuildArtifact{
			{App: "vfd", Name: "engine", Lang: "cpp", Cmd: "/home/apps/vfd/engine/run.sh",
				Kind: "dir", Port: 8787, Expose: "loopback", Autostart: true,
				Env: map[string]string{"MI_PORT": "8787", "MI_HOST": "127.0.0.1"}},
			{App: "vfd", Name: "console", Lang: "node-webroot", Kind: "webroot"},
			{App: "wiki", Name: "site", Lang: "node-webroot", Cmd: "/home/apps/wiki/run.sh",
				Kind: "webroot", Expose: "lan", Port: 9000},
		},
	}

	artifacts := []BuiltArtifact{
		{Name: "engine", Arch: "arm64-v8a", Kind: "dir",
			URL: "https://ghcr.io/v2/owner/vfd/blobs/sha256:" + shaEngine, SHA256: shaEngine, Size: 1882104},
		{Name: "console", Arch: "all", Kind: "webroot",
			URL: "https://ghcr.io/v2/owner/vfd/blobs/sha256:" + shaConsole, SHA256: shaConsole},
		{Name: "site", Arch: "all", Kind: "webroot",
			URL: "https://ghcr.io/v2/owner/vfd/blobs/sha256:" + shaConsole, SHA256: shaConsole},
	}

	out, err := RenderManifest(recipe, artifacts, ManifestTarget{DeviceID: "d_01H", Org: "org_01H"})
	if err != nil {
		t.Fatalf("RenderManifest = %v", err)
	}

	// The rendered manifest must be schema-valid, or the deploy that follows is a
	// server refusal. Validate against the published manifest schema.
	if err := validateRendered(t, out); err != nil {
		t.Fatalf("rendered manifest is not valid: %v\n%s", err, out)
	}

	var doc struct {
		Device struct {
			ID  string `toml:"id"`
			Org string `toml:"org"`
		} `toml:"device"`
		App []struct {
			Name      string            `toml:"name"`
			Cmd       string            `toml:"cmd"`
			Port      int               `toml:"port"`
			Expose    string            `toml:"expose"`
			Autostart bool              `toml:"autostart"`
			Env       map[string]string `toml:"env"`
			Artifact  []struct {
				Name   string `toml:"name"`
				Arch   string `toml:"arch"`
				URL    string `toml:"url"`
				Sha256 string `toml:"sha256"`
				Kind   string `toml:"kind"`
			} `toml:"artifact"`
		} `toml:"app"`
	}

	if err := toml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("rendered manifest is not valid TOML: %v", err)
	}

	if doc.Device.ID != "d_01H" || doc.Device.Org != "org_01H" {
		t.Errorf("device = %+v, want d_01H/org_01H", doc.Device)
	}

	if len(doc.App) != 2 {
		t.Fatalf("got %d apps, want 2 (the two vfd artifacts must group)", len(doc.App))
	}

	// Order is significant: vfd first (it appears first), then wiki.
	if doc.App[0].Name != "vfd" || doc.App[1].Name != "wiki" {
		t.Errorf("app order = %q,%q, want vfd,wiki", doc.App[0].Name, doc.App[1].Name)
	}

	if len(doc.App[0].Artifact) != 2 {
		t.Fatalf("vfd has %d artifacts, want 2 grouped under one app", len(doc.App[0].Artifact))
	}

	// The engine is the ABI-specific artifact: it must carry its arch and the
	// digest the build produced.
	engine := doc.App[0].Artifact[0]
	if engine.Name != "engine" || engine.Sha256 != shaEngine || engine.Arch != "arm64-v8a" {
		t.Errorf("engine = %+v, want name/arch/sha256 from the build", engine)
	}

	if !strings.Contains(engine.URL, shaEngine) {
		t.Errorf("engine url %q does not carry the digest", engine.URL)
	}

	// The console is architecture-free, so its arch segment is emitted as "all"
	// by the builder, and the manifest omits it (arch is optional for a webroot).
	console := doc.App[0].Artifact[1]
	if console.Name != "console" || console.Sha256 != shaConsole {
		t.Errorf("console = %+v, want the console artifact", console)
	}

	if console.Arch != "" {
		t.Errorf("console arch = %q, want omitted for an architecture-free artifact", console.Arch)
	}

	// App-level fields come from the group's artifacts.
	app := doc.App[0]
	if app.Cmd != "/home/apps/vfd/engine/run.sh" || app.Port != 8787 || app.Expose != "loopback" || !app.Autostart {
		t.Errorf("vfd app = %+v, want the engine's cmd/port/expose/autostart", app)
	}

	if app.Env["MI_PORT"] != "8787" || app.Env["MI_HOST"] != "127.0.0.1" {
		t.Errorf("vfd env = %+v, want the engine's env", app.Env)
	}
}

// TestRenderManifestRefusesMissingCmd proves an app with no cmd is refused before
// a manifest is written, because a manifest without a cmd is a server refusal.
func TestRenderManifestRefusesMissingCmd(t *testing.T) {
	recipe := &vyzorixspec.BuildRecipe{
		Artifact: []vyzorixspec.BuildArtifact{{App: "vfd", Name: "engine", Lang: "cpp"}},
	}

	_, err := RenderManifest(recipe, []BuiltArtifact{{Name: "engine", SHA256: shaEngine}}, ManifestTarget{DeviceID: "d", Org: "o"})
	if err == nil || !strings.Contains(err.Error(), "cmd") {
		t.Fatalf("RenderManifest = %v, want a refusal naming cmd", err)
	}
}

// TestRenderManifestRefusesMissingArtifact proves a digest the builder did not
// produce is a refusal, not a silent empty sha256 that would deploy and then fail
// on the device.
func TestRenderManifestRefusesMissingArtifact(t *testing.T) {
	recipe := &vyzorixspec.BuildRecipe{
		Artifact: []vyzorixspec.BuildArtifact{
			{App: "vfd", Name: "engine", Lang: "cpp", Cmd: "/home/apps/vfd/run.sh"},
		},
	}

	_, err := RenderManifest(recipe, nil, ManifestTarget{DeviceID: "d", Org: "o"})
	if err == nil || !strings.Contains(err.Error(), "engine") {
		t.Fatalf("RenderManifest = %v, want a refusal naming the missing artifact", err)
	}
}

// TestRenderManifestIsDeterministic pins that the same recipe and build render
// byte-identical output, which is what makes a manifest a function of the build.
func TestRenderManifestIsDeterministic(t *testing.T) {
	recipe := &vyzorixspec.BuildRecipe{
		Artifact: []vyzorixspec.BuildArtifact{
			{App: "vfd", Name: "engine", Lang: "cpp", Cmd: "/home/apps/vfd/run.sh",
				Env: map[string]string{"B": "2", "A": "1", "C": "3"}},
		},
	}

	artifacts := []BuiltArtifact{{Name: "engine", Arch: "arm64-v8a", URL: "https://e/engine", SHA256: shaEngine}}

	first, err := RenderManifest(recipe, artifacts, ManifestTarget{DeviceID: "d", Org: "o"})
	if err != nil {
		t.Fatalf("first render = %v", err)
	}

	second, err := RenderManifest(recipe, artifacts, ManifestTarget{DeviceID: "d", Org: "o"})
	if err != nil {
		t.Fatalf("second render = %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("renders differ:\n%s\n---\n%s", first, second)
	}
}

func TestRenderManifestPwaPrecedesEnvTable(t *testing.T) {
	// A bare app key must precede the [app.env] subtable. If `pwa` were emitted
	// after the table header, TOML would scope it inside app.env and the server
	// would refuse the manifest ("env/pwa: got boolean, want string"). This pins
	// the ordering: pwa is set AND env is set, so a regression re-triggers it.
	recipe := &vyzorixspec.BuildRecipe{
		Build: vyzorixspec.BuildSettings{Version: "v1.2.0"},
		Artifact: []vyzorixspec.BuildArtifact{
			{App: "vfd", Name: "engine", Lang: "cpp", Cmd: "/home/apps/vfd/run.sh",
				Kind: "dir", Pwa: true, Env: map[string]string{"MI_PORT": "8787"}},
		},
	}

	artifacts := []BuiltArtifact{
		{Name: "engine", Arch: "arm64-v8a", Kind: "dir",
			URL: "https://ghcr.io/v2/owner/vfd/blobs/sha256:" + shaEngine, SHA256: shaEngine},
	}

	out, err := RenderManifest(recipe, artifacts, ManifestTarget{DeviceID: "d_01H", Org: "org_01H"})
	if err != nil {
		t.Fatalf("RenderManifest = %v", err)
	}

	// The rendered manifest must be schema-valid; a misplaced pwa fails here.
	if err := validateRendered(t, out); err != nil {
		t.Fatalf("rendered manifest is not valid: %v\n%s", err, out)
	}

	var doc struct {
		App []struct {
			Pwa bool              `toml:"pwa"`
			Env map[string]string `toml:"env"`
		} `toml:"app"`
	}
	if err := toml.Unmarshal(out, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(doc.App) != 1 || !doc.App[0].Pwa {
		t.Fatalf("pwa must be a top-level app key, got %+v", doc.App)
	}
	if doc.App[0].Env["MI_PORT"] != "8787" {
		t.Fatalf("env must survive, got %+v", doc.App[0].Env)
	}
}

// validateRendered checks the rendered manifest against the published manifest
// schema, through the same validator the deploy command uses.
func validateRendered(t *testing.T, manifest []byte) error {
	t.Helper()

	schema, err := vyzorixspec.Schema(vyzorixspec.SchemaVersion)
	if err != nil {
		return err
	}

	var doc map[string]any
	if err := toml.Unmarshal(manifest, &doc); err != nil {
		return err
	}

	encoded, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		return err
	}

	return validateDocument("vyzorix.toml", schema, decoded, "vyzorix.toml")
}
