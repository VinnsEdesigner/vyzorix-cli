package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/VinnsEdesigner/vyzorix-cli/internal/config"
	deploycli "github.com/VinnsEdesigner/vyzorix-cli/internal/deploy"
	"github.com/VinnsEdesigner/vyzorix-cli/internal/ui"
)

// Exit codes. Zero is success; a refusal from the server is 1, with the server's
// reason printed verbatim; a local failure (bad flags, unreadable file, an
// unreachable server) is 2, so a caller can tell "the server said no" from "the
// command never reached the server".
const (
	exitOK      = 0
	exitRefused = 1
	exitUsage   = 2
)

// version is the released build's version, injected at build time with
// -ldflags "-X main.version=v0.1.0". A build from a working tree reports "dev",
// so an unreleased binary cannot be mistaken for a tagged one.
var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr *os.File) int {
	if len(args) == 0 {
		usage(stderr)

		return exitUsage
	}

	ctx := context.Background()

	switch args[0] {
	case "login":
		return runLogin(ctx, args[1:], stdout, stderr)
	case "logout":
		return runLogout(stdout, stderr)
	case "whoami":
		return runWhoami(ctx, args[1:], stdout, stderr)
	case "deploy":
		return runDeploy(ctx, args[1:], stdout, stderr)
	case "build":
		return runBuild(ctx, args[1:], stdout, stderr)
	case "secrets":
		return runSecrets(ctx, args[1:], stdout, stderr)
	case "-v", "--version", "version":
		_, _ = fmt.Fprintf(stdout, "vyzorix %s\n", version)

		return exitOK
	case "-h", "--help", "help":
		usage(stdout)

		return exitOK
	default:
		ui.New(stderr).Error("unknown command %q", args[0])
		_, _ = fmt.Fprintln(stderr)
		usage(stderr)

		return exitUsage
	}
}

func runLogin(ctx context.Context, args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(stderr)

	server := fs.String("server", "", "server base URL (or VYZORIX_SERVER)")
	org := fs.String("org", "", "organization id (or VYZORIX_ORG)")
	device := fs.String("device", "", "optional device id, to show its tier")
	apiKey := fs.String("api-key", "", "operator API key (or VYZORIX_API_KEY)")
	credential := fs.String("deploy-credential", "", "device-scoped CI token (or VYZORIX_DEPLOY_CREDENTIAL)")

	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	saved, err := config.Load()
	if err != nil {
		ui.New(stderr).Error("could not read config: %v", err)

		return exitUsage
	}

	base := resolve(*server, "VYZORIX_SERVER", saved.Server)
	if base == "" {
		ui.New(stderr).Error("--server or VYZORIX_SERVER is required")

		return exitUsage
	}

	auth := deploycli.Auth{
		APIKey:           resolve(*apiKey, "VYZORIX_API_KEY", ""),
		DeployCredential: resolve(*credential, "VYZORIX_DEPLOY_CREDENTIAL", ""),
	}
	if !auth.Valid() {
		ui.New(stderr).Error("provide exactly one of --api-key or --deploy-credential")

		return exitUsage
	}

	orgID := resolve(*org, "VYZORIX_ORG", "")

	client := deploycli.NewClient(base, orgID, auth)
	printer := ui.New(stdout)

	printer.Step("Signing in to %s", base)

	identity, err := client.Whoami(ctx, *device)
	if err != nil {
		return reportError(err, stderr)
	}

	// An API key carries no org, so the server's answer is the org to save. A
	// deploy credential already carried one.
	if orgID == "" {
		orgID = identity.OrgID
	}

	printer.Success("Signed in")
	printer.Blank()
	deploycli.PrintIdentity(printer, identity)

	if err := config.Save(&config.File{
		Server:     base,
		Org:        orgID,
		APIKey:     auth.APIKey,
		Credential: auth.DeployCredential,
	}); err != nil {
		ui.New(stderr).Error("could not save config: %v", err)

		return exitUsage
	}

	if path, err := config.Path(); err == nil {
		printer.Blank()
		printer.Detail("Saved to %s", path)
	}

	return exitOK
}

func runLogout(stdout, stderr *os.File) int {
	if err := config.Remove(); err != nil {
		ui.New(stderr).Error("could not remove config: %v", err)

		return exitUsage
	}

	ui.New(stdout).Success("Signed out")

	return exitOK
}

func runWhoami(ctx context.Context, args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("whoami", flag.ContinueOnError)
	fs.SetOutput(stderr)

	server := fs.String("server", "", "server base URL (or VYZORIX_SERVER)")
	org := fs.String("org", "", "organization id (or VYZORIX_ORG)")
	device := fs.String("device", "", "optional device id, to show its tier")

	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	base, orgID, auth, ok := resolveSettings(*server, *org, stderr)
	if !ok {
		return exitUsage
	}

	if err := requireAuth(base, orgID, auth); err != nil {
		ui.New(stderr).Error("%s", err)

		return exitUsage
	}

	client := deploycli.NewClient(base, orgID, auth)
	printer := ui.New(stdout)

	identity, err := client.Whoami(ctx, *device)
	if err != nil {
		return reportError(err, stderr)
	}

	deploycli.PrintIdentity(printer, identity)

	return exitOK
}

// runDeploy implements `vyzorix deploy --org ORG --device DEV [--file vyzorix.toml]`.
func runDeploy(ctx context.Context, args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("deploy", flag.ContinueOnError)
	fs.SetOutput(stderr)

	org := fs.String("org", "", "organization id (or VYZORIX_ORG)")
	device := fs.String("device", "", "target device id (required)")
	file := fs.String("file", "vyzorix.toml", "path to the manifest")
	server := fs.String("server", "", "server base URL (or VYZORIX_SERVER)")
	dryRun := fs.Bool("dry-run", false, "compile and authorise, but do not deliver")
	build := fs.Bool("build", false, "run `vyzorix build` first, then deploy the result")
	recipe := fs.String("recipe", "vyzorix.build.toml", "path to the build recipe (with --build)")

	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	base, orgID, auth, ok := resolveSettings(*server, *org, stderr)
	if !ok {
		return exitUsage
	}

	// `--build` builds first, then deploys what it wrote. The two commands' --file
	// flags mean different files, so the build's arguments are composed explicitly:
	// the recipe in, the deploy's manifest out.
	if *build {
		buildArgs := []string{"--device", *device, "--file", *recipe, "--manifest-out", *file}

		if *server != "" {
			buildArgs = append(buildArgs, "--server", *server)
		}

		if *org != "" {
			buildArgs = append(buildArgs, "--org", *org)
		}

		if code := runBuild(ctx, buildArgs, stdout, stderr); code != exitOK {
			return code
		}
	}

	if err := requireDeploy(base, orgID, *device, auth); err != nil {
		ui.New(stderr).Error("%s", err)

		return exitUsage
	}

	manifest, err := os.ReadFile(*file)
	if err != nil {
		ui.New(stderr).Error("could not read %s: %v", *file, err)

		return exitUsage
	}

	client := deploycli.NewClient(base, orgID, auth)
	printer := ui.New(stdout)

	printer.Step("Deploying to %s", *device)

	result, err := client.Deploy(ctx, manifest, deploycli.DeployOptions{
		Org:    orgID,
		Device: *device,
		DryRun: *dryRun,
	}, printer)
	if err != nil {
		return reportError(err, stderr)
	}

	deploycli.PrintReport(printer, result)

	return exitOK
}

// runBuild implements `vyzorix build --device DEV [--file vyzorix.build.toml]`.
func runBuild(ctx context.Context, args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)

	org := fs.String("org", "", "organization id (or VYZORIX_ORG)")
	device := fs.String("device", "", "target device id (needed unless the recipe sets abi)")
	file := fs.String("file", "vyzorix.build.toml", "path to the build recipe")
	out := fs.String("manifest-out", "vyzorix.toml", "where to write the rendered manifest")
	server := fs.String("server", "", "server base URL (or VYZORIX_SERVER)")
	version := fs.String("version", "", "artifact version (or the recipe's build.version)")
	projectDir := fs.String("project-dir", ".", "the project root the recipe's paths are relative to")
	publish := fs.String("publish", "ghcr", "publish backend (ghcr)")
	namespace := fs.String("namespace", "", "registry namespace (GHCR owner) (or VYZORIX_BUILD_NAMESPACE)")
	repo := fs.String("repo", "", "registry repository (or VYZORIX_BUILD_REPO)")
	dryRun := fs.Bool("dry-run", false, "build, but publish nothing")

	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	base, orgID, auth, ok := resolveSettings(*server, *org, stderr)
	if !ok {
		return exitUsage
	}

	// The recipe's `abi` may make --device optional, but a credential is still
	// needed to resolve the ABIs from the server when it does not.
	if err := requireAuth(base, orgID, auth); err != nil {
		ui.New(stderr).Error("%s", err)

		return exitUsage
	}

	recipe, err := os.ReadFile(*file)
	if err != nil {
		ui.New(stderr).Error("could not read %s: %v", *file, err)

		return exitUsage
	}

	client := deploycli.NewClient(base, orgID, auth)
	printer := ui.New(stdout)

	printer.Step("Building for %s", deviceLabel(*device))

	outcome, err := client.Build(ctx, recipe, deploycli.BuildOptions{
		Version:      *version,
		Device:       *device,
		Org:          orgID,
		ManifestPath: *out,
		ProjectDir:   *projectDir,
		DryRun:       *dryRun,
		Publish: deploycli.Publish{
			Backend:    *publish,
			Namespace:  resolve(*namespace, "VYZORIX_BUILD_NAMESPACE", ""),
			Repo:       resolve(*repo, "VYZORIX_BUILD_REPO", ""),
			Visibility: "public",
		},
	}, printer)
	if err != nil {
		return reportError(err, stderr)
	}

	deploycli.PrintBuildReport(printer, outcome)

	return exitOK
}

// deviceLabel renders a device id for a step line, tolerating its absence.
func deviceLabel(device string) string {
	if device == "" {
		return "the recipe's abi"
	}

	return device
}

// runSecrets implements `vyzorix secrets set|list|unset`.
func runSecrets(ctx context.Context, args []string, stdout, stderr *os.File) int {
	if len(args) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: vyzorix secrets set|list|unset ...")

		return exitUsage
	}

	switch args[0] {
	case "set":
		return runSecretsSet(ctx, args[1:], stdout, stderr)
	case "list":
		return runSecretsList(ctx, args[1:], stdout, stderr)
	case "unset":
		return runSecretsUnset(ctx, args[1:], stdout, stderr)
	default:
		ui.New(stderr).Error("unknown secrets command %q", args[0])

		return exitUsage
	}
}

func runSecretsSet(ctx context.Context, args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("secrets set", flag.ContinueOnError)
	fs.SetOutput(stderr)

	device := fs.String("device", "", "target device id (required)")
	server := fs.String("server", "", "server base URL (or VYZORIX_SERVER)")
	org := fs.String("org", "", "organization id (or VYZORIX_ORG)")

	rest, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(rest) != 2 {
		_, _ = fmt.Fprintln(stderr, "usage: vyzorix secrets set --device <id> --org <id> <app> KEY=VALUE")

		return exitUsage
	}

	app := rest[0]

	key, value, err := deploycli.SplitAssignment(rest[1])
	if err != nil {
		ui.New(stderr).Error("%s", err)

		return exitUsage
	}

	base, orgID, auth, ok := resolveSettings(*server, *org, stderr)
	if !ok {
		return exitUsage
	}

	if err := requireDeploy(base, orgID, *device, auth); err != nil {
		ui.New(stderr).Error("%s", err)

		return exitUsage
	}

	client := deploycli.NewClient(base, orgID, auth)
	printer := ui.New(stdout)

	if err := client.SetSecret(ctx, *device, app, key, value); err != nil {
		return reportError(err, stderr)
	}

	// The value is never echoed; the name is what confirms the write.
	printer.Success("Set %s in %s", key, app)

	return exitOK
}

func runSecretsList(ctx context.Context, args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("secrets list", flag.ContinueOnError)
	fs.SetOutput(stderr)

	device := fs.String("device", "", "target device id (required)")
	server := fs.String("server", "", "server base URL (or VYZORIX_SERVER)")
	org := fs.String("org", "", "organization id (or VYZORIX_ORG)")

	rest, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(rest) != 1 {
		_, _ = fmt.Fprintln(stderr, "usage: vyzorix secrets list --device <id> --org <id> <app>")

		return exitUsage
	}

	base, orgID, auth, ok := resolveSettings(*server, *org, stderr)
	if !ok {
		return exitUsage
	}

	if err := requireDeploy(base, orgID, *device, auth); err != nil {
		ui.New(stderr).Error("%s", err)

		return exitUsage
	}

	client := deploycli.NewClient(base, orgID, auth)
	printer := ui.New(stdout)

	printer.Step("Secrets for %s", rest[0])

	if err := client.ListSecrets(ctx, *device, rest[0], printer); err != nil {
		return reportError(err, stderr)
	}

	return exitOK
}

func runSecretsUnset(ctx context.Context, args []string, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("secrets unset", flag.ContinueOnError)
	fs.SetOutput(stderr)

	device := fs.String("device", "", "target device id (required)")
	server := fs.String("server", "", "server base URL (or VYZORIX_SERVER)")
	org := fs.String("org", "", "organization id (or VYZORIX_ORG)")

	rest, err := parseInterspersed(fs, args)
	if err != nil {
		return exitUsage
	}
	if len(rest) != 2 {
		_, _ = fmt.Fprintln(stderr, "usage: vyzorix secrets unset --device <id> --org <id> <app> KEY")

		return exitUsage
	}

	base, orgID, auth, ok := resolveSettings(*server, *org, stderr)
	if !ok {
		return exitUsage
	}

	if err := requireDeploy(base, orgID, *device, auth); err != nil {
		ui.New(stderr).Error("%s", err)

		return exitUsage
	}

	client := deploycli.NewClient(base, orgID, auth)
	printer := ui.New(stdout)

	if err := client.UnsetSecret(ctx, *device, rest[0], rest[1]); err != nil {
		return reportError(err, stderr)
	}

	printer.Success("Unset %s in %s", rest[1], rest[0])

	return exitOK
}

// reportError prints an error and picks the exit code: a server refusal is 1 with
// the server's reason verbatim; anything else is 2.
func reportError(err error, stderr *os.File) int {
	ui.New(stderr).Error("%s", err)

	if deploycli.IsRefusal(err) {
		return exitRefused
	}

	return exitUsage
}

// resolveSettings resolves the server, org and credential for a command from the
// flag, then the environment, then the saved config — the order a user expects:
// an explicit flag wins, an environment variable is next, and a saved login is
// the fallback. It reports false (having printed the cause) when the config
// cannot be read.
func resolveSettings(serverFlag, orgFlag string, stderr *os.File) (string, string, deploycli.Auth, bool) {
	saved, err := config.Load()
	if err != nil {
		ui.New(stderr).Error("could not read config: %v", err)

		return "", "", deploycli.Auth{}, false
	}

	server := resolve(serverFlag, "VYZORIX_SERVER", saved.Server)
	org := resolve(orgFlag, "VYZORIX_ORG", saved.Org)

	auth := deploycli.Auth{
		APIKey:           resolve("", "VYZORIX_API_KEY", saved.APIKey),
		DeployCredential: resolve("", "VYZORIX_DEPLOY_CREDENTIAL", saved.Credential),
	}

	return server, org, auth, true
}

// resolve picks the first non-empty value: an explicit flag, then an environment
// variable, then the saved config.
func resolve(flagValue, envKey, saved string) string {
	if flagValue != "" {
		return flagValue
	}

	if v := os.Getenv(envKey); v != "" {
		return v
	}

	return saved
}

// requireAuth checks the two values every authenticated command needs.
func requireAuth(server, org string, auth deploycli.Auth) error {
	if server == "" {
		return errors.New("--server or VYZORIX_SERVER is required; run `vyzorix login`")
	}

	if org == "" {
		return errors.New("--org or VYZORIX_ORG is required; run `vyzorix login`")
	}

	if !auth.Valid() {
		return errors.New("no credential: run `vyzorix login` or set VYZORIX_API_KEY or VYZORIX_DEPLOY_CREDENTIAL")
	}

	return nil
}

// requireDeploy is requireAuth plus the device a deploy or secret write needs.
func requireDeploy(server, org, device string, auth deploycli.Auth) error {
	if err := requireAuth(server, org, auth); err != nil {
		return err
	}

	if device == "" {
		return errors.New("--device is required")
	}

	return nil
}

// parseInterspersed parses flags that may be interleaved with positional
// arguments. The flag package stops at the first positional argument, but the
// contract documents `secrets set <app> KEY=VALUE --device <id>` (§9b.6), so the
// positionals are collected while the remaining `-flag` arguments are still
// parsed.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string

	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}

		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}

		positional = append(positional, args[0])
		args = args[1:]
	}
}

func usage(out *os.File) {
	_, _ = fmt.Fprint(out, `Vyzorix Deploy CLI - push manifests and secrets to your fleet

  This is the deploy side. To reach a device's own APIs (camera, battery, wifi,
  apps) run the Vyzorix CLI inside the device's container.

  Install:
    curl -fsSL https://raw.githubusercontent.com/VinnsEdesigner/vyzorix-cli/main/install.sh | sh
    # Or with Go 1.27+:
    go install github.com/VinnsEdesigner/vyzorix-cli/cmd/vyzorix@latest

  Usage:
    vyzorix login   --server URL --api-key KEY [--org ORG] [--device DEV]
    vyzorix whoami  [--device DEV]
    vyzorix logout
    vyzorix build   --device DEV [--file vyzorix.build.toml] [--manifest-out vyzorix.toml]
    vyzorix deploy  --device DEV [--org ORG] [--file vyzorix.toml] [--dry-run] [--build]
    vyzorix secrets set   --device DEV <app> KEY=VALUE
    vyzorix secrets list  --device DEV <app>
    vyzorix secrets unset --device DEV <app> KEY

  Run vyzorix login once. It saves the server, the org and the credential, so
  deploy and secrets need only the device. A flag or an environment variable
  overrides a saved value.

  'vyzorix build' delegates to vyzorix-builder (found via $VYZORIX_BUILDER or on
  PATH) to compile and publish the recipe's artifacts, then renders the manifest
  from what it produced. 'vyzorix deploy --build' runs both.

  Flags come before the positional arguments.

  Environment:
    VYZORIX_SERVER              server base URL
    VYZORIX_ORG                 organization id
    VYZORIX_API_KEY             operator API key (reaches many devices)
    VYZORIX_DEPLOY_CREDENTIAL   device-scoped CI token (reaches one device)
    VYZORIX_CONFIG              config file path (default ~/.config/vyzorix/config.toml)
    VYZORIX_BUILDER             path to vyzorix-builder
    VYZORIX_BUILD_NAMESPACE     registry namespace for --publish
    VYZORIX_BUILD_REPO          registry repository for --publish

  Set exactly one credential.
  `)
}
