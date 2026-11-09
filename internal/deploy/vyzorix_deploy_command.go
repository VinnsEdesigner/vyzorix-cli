package deploy

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/VinnsEdesigner/vyzorix-cli/internal/ui"
)

// DeployResponse mirrors the part of the server's POST /v1/deploy body the CLI
// reports on. It is declared here rather than taken from the generated package
// because the generated types are the request side of the manifest; the deploy
// *response* is a handler type and is not part of the published schema. The
// signed document is fetched from GET /v1/deploy/{device}/document, so it is not
// decoded here.
type DeployResponse struct {
	AuthorizedBy string       `json:"authorizedBy"`
	Apps         []AppMarking `json:"apps"`
	Artifacts    int          `json:"artifacts"`
	Version      int          `json:"version"`
	DryRun       bool         `json:"dryRun"`
}

// AppMarking is one app's catalog classification (§8.1): custom means the
// catalog did not know it.
type AppMarking struct {
	Name   string `json:"name"`
	Custom bool   `json:"custom"`
}

// DeployOptions is one `vyzorix deploy` invocation.
type DeployOptions struct {
	Org    string
	Device string
	DryRun bool
}

// Deploy runs the whole command: load, validate against the published schema,
// print the summary, then upload. A dry run stops after the server compiles and
// signs, which is where US-009 draws the line.
//
// p receives the human-readable progress. Errors are returned, not printed, so
// the entry point owns the exit code.
func (c *Client) Deploy(ctx context.Context, manifestData []byte, opts DeployOptions, p *ui.Printer) (*DeployResponse, error) {
	manifest, err := LoadManifest(manifestData)
	if err != nil {
		return nil, err
	}

	// The manifest's device and org are the declaration; the flags are the
	// invocation. They must agree, or the CLI would validate one thing and upload
	// another.
	if manifest.Typed.Device.ID != "" && opts.Device != "" && manifest.Typed.Device.ID != opts.Device {
		return nil, fmt.Errorf("vyzorix.toml targets device %q, but --device is %q", manifest.Typed.Device.ID, opts.Device)
	}

	if manifest.Typed.Device.Org != "" && opts.Org != "" && manifest.Typed.Device.Org != opts.Org {
		return nil, fmt.Errorf("vyzorix.toml targets org %q, but --org is %q", manifest.Typed.Device.Org, opts.Org)
	}

	schema, err := c.Schema(ctx)
	if err != nil {
		return nil, fmt.Errorf("could not fetch the manifest schema: %w", err)
	}

	if err := Validate(c.SchemaURL(), schema, manifest); err != nil {
		return nil, err
	}

	printSummary(p, manifest)

	path := "/v1/deploy"
	if opts.DryRun {
		path += "?dry_run=true"
	}

	var resp DeployResponse
	if err := c.do(ctx, http.MethodPost, path, manifest.Raw, "application/toml", &resp); err != nil {
		return nil, err
	}

	return &resp, nil
}

// printSummary prints what the manifest declares before the upload: the apps
// and their artifacts. The version is the server's to assign, so it is reported
// from the response, not guessed here.
func printSummary(p *ui.Printer, manifest *Manifest) {
	p.Detail("%d app(s)", len(manifest.Typed.App))

	for _, app := range manifest.Typed.App {
		p.Detail("  %s", app.Name)

		for _, art := range app.Artifact {
			size := "size unknown"
			if art.Size != nil {
				size = humanBytes(*art.Size)
			}

			arch := art.Arch
			if arch == "" {
				arch = "any"
			}

			p.Detail("    %s  %s  %s", art.Name, arch, size)
		}
	}
}

// humanBytes formats a byte count for a terminal.
func humanBytes(n int64) string {
	const unit = 1024

	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// PrintReport writes the outcome of a deploy: the version, the authoriser and
// the app classification, so the receipt can be checked against the device's
// config (§3).
func PrintReport(p *ui.Printer, resp *DeployResponse) {
	if resp.DryRun {
		p.Success("Dry run: version %d would be deployed", resp.Version)
	} else {
		p.Success("Deployed version %d", resp.Version)
	}

	p.Fields([][2]string{
		{"authorised by", resp.AuthorizedBy},
		{"artifacts", fmt.Sprintf("%d", resp.Artifacts)},
	})

	rows := make([][]string, 0, len(resp.Apps))
	for _, app := range resp.Apps {
		marking := "known"
		if app.Custom {
			marking = "custom"
		}

		rows = append(rows, []string{app.Name, marking})
	}

	if len(rows) > 0 {
		p.Blank()
		p.Table([]string{"APP", "SOURCE"}, rows)
	}
}

// IsRefusal reports whether err is a server refusal rather than a local failure,
// so the entry point can choose the exit code.
func IsRefusal(err error) bool {
	var apiErr *APIError

	return errors.As(err, &apiErr)
}
