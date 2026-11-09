# vyzorix-cli

The Vyzorix deployment CLI. It validates a `vyzorix.toml` manifest against the
server's published schema, uploads it, and drives the device secret surface.

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
vyzorix deploy --device DEV [--org ORG] [--file vyzorix.toml] [--dry-run]
vyzorix secrets set   --device DEV <app> KEY=VALUE
vyzorix secrets list  --device DEV <app>
vyzorix secrets unset --device DEV <app> KEY
```

Run `vyzorix login` once: it and it's saved to the server, the org and the credential to
`~/.config/vyzorix/config.toml` (mode 0600), so `deploy` and `secrets` need only
the device. A flag or an environment variable overrides a saved value.

Exit codes: `0` success, `1` the server refused, `2` usage or a local failure.

## Design

- **The manifest schema is fetched, not embedded.** The CLI validates against
  `GET /v1/deploy/schema/{version}` so a self-hosted server is authoritative for
  its own clients. It pins the version, so a manifest it accepted is a manifest
  the server accepts.
- **The wire types come from `vyzorix-spec`.** The CLI and the server share one
  declaration of the manifest shape, so neither can drift.
- **The credential is proved before it is saved.** `login` calls the server
  first, so a bad key leaves no config behind.

## Releases

`.github/workflows/release.yml` publishes `linux/amd64`, `linux/arm64`,
`darwin/amd64`, `darwin/arm64` and `windows/amd64` plus `SHA256SUMS` on a `v*`
tag. The installer verifies the checksum before installing. The build is
CGO-free, so all five targets come from one runner.

## License

GPL-3.0. See `LICENSE`.
