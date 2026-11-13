# vyzorix-cli

The **Vyzorix Deploy CLI**. It validates a `vyzorix.toml` manifest against the
server's published schema, uploads it, and drives the device secret surface.

This is the deploy side, run from your machine or CI. To reach a device's own
APIs from the device itself, that is the **Vyzorix CLI** inside the container.

The server is authoritative: the CLI compiles nothing and never re-serialises
the manifest. It pre-validates so a schema error is caught locally with a
readable field path rather than by the server as a refusal.

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/VinnsEdesigner/vyzorix-cli/main/install.sh | sh
```

Or, with Go 1.27+:

```sh
go install github.com/VinnsEdesigner/vyzorix-cli/cmd/vyzorix@latest
```

Both place `vyzorix` on your PATH (`~/.local/bin` for the script, `$(go env
GOPATH)/bin` for `go install`). Set `PREFIX` to change the script's target, and
`VYZORIX_VERSION` to pin a release.

## Use

```sh
vyzorix login  --server https://your-server --api-key KEY [--org ORG] [--device DEV]
vyzorix whoami [--device DEV]
vyzorix build  --device DEV [--file vyzorix.build.toml] [--manifest-out vyzorix.toml]
vyzorix deploy --device DEV [--org ORG] [--file vyzorix.toml] [--dry-run] [--build]
vyzorix secrets set   --device DEV <app> KEY=VALUE
vyzorix secrets list  --device DEV <app>
vyzorix secrets unset --device DEV <app> KEY
```

Run `vyzorix login` once: it and it's saved to the server, the org and the credential to
`~/.config/vyzorix/config.toml` (mode 0600), so `deploy` and `secrets` need only
the device. A flag or an environment variable overrides a saved value.

Exit codes: `0` success, `1` the server refused, `2` usage or a local failure.

## Building

`vyzorix build` reads a `vyzorix.build.toml` recipe, resolves the target
architecture from the device, delegates to **`vyzorix-builder`** (a separate
binary, found via `$VYZORIX_BUILDER` or on `PATH`), and renders the
`vyzorix.toml` manifest from what the builder produced. The CLI compiles nothing
itself, so its release stays CGO-free and small. `vyzorix deploy --build` runs
both. See `VYZORIX/DOCUMENTATION/BUILD_TOOL_CONTRACT.md`.

## Design

- **The manifest schema is fetched, not embedded.** The CLI validates against
  `GET /v1/deploy/schema/{version}` so a self-hosted server is authoritative for
  its own clients. It pins the version, so a manifest it accepted is a manifest
  the server accepts.
- **The wire types come from `vyzorix-spec`.** The CLI and the server share one
  declaration of the manifest shape (and, since `v0.2.0`, the build recipe), so
  neither can drift. The `build` command needs `vyzorix-spec` **v0.2.0 or later**;
  the module is tagged and published, so `go build` resolves it without a
  `replace`.
- **The credential is proved before it is saved.** `login` calls the server
  first, so a bad key leaves no config behind.

## Releases

`.github/workflows/release.yml` publishes `linux/amd64`, `linux/arm64`,
`darwin/amd64`, `darwin/arm64` and `windows/amd64` plus `SHA256SUMS` on a `v*`
tag. The installer verifies the checksum before installing. The build is
CGO-free, so all five targets come from one runner.

## License

GPL-3.0. See `LICENSE`.
