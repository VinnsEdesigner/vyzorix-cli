package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests run the real run() over the real config file, so the login ->
// save -> whoami path is exercised end to end rather than through its parts.

// identityServer serves GET /v1/deploy/whoami with a fixed identity and records
// the credential header it was presented.
func identityServer(t *testing.T) (*httptest.Server, *string) {
	t.Helper()

	var presented string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/deploy/whoami" {
			t.Errorf("path = %q, want /v1/deploy/whoami", r.URL.Path)

			return
		}

		presented = r.Header.Get("X-API-Key")

		_ = json.NewEncoder(w).Encode(map[string]any{
			"operatorName": "Ada Lovelace",
			"email":        "ada@example.test",
			"role":         "admin",
			"orgId":        "org_01H",
			"orgName":      "Acme Devices",
			"device":       map[string]string{"id": "d_01H", "name": "Pixel", "tier": "rooted"},
		})
	}))

	t.Cleanup(srv.Close)

	return srv, &presented
}

// runCapture runs the CLI with the given args, capturing stdout and stderr.
func runCapture(t *testing.T, args ...string) (int, string, string) {
	t.Helper()

	outFile, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatalf("temp out: %v", err)
	}

	errFile, err := os.CreateTemp(t.TempDir(), "err")
	if err != nil {
		t.Fatalf("temp err: %v", err)
	}

	code := run(args, outFile, errFile)

	out, _ := os.ReadFile(outFile.Name())
	errOut, _ := os.ReadFile(errFile.Name())

	return code, string(out), string(errOut)
}

// TestLoginSavesThenWhoamiReadsIt is the install flow's core: login saves the
// server, org and credential; a later whoami runs with no flags at all, because
// it reads them from the saved config.
func TestLoginSavesThenWhoamiReadsIt(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("VYZORIX_CONFIG", configPath)
	t.Setenv("VYZORIX_SERVER", "")
	t.Setenv("VYZORIX_ORG", "")
	t.Setenv("VYZORIX_API_KEY", "")
	t.Setenv("VYZORIX_DEPLOY_CREDENTIAL", "")

	srv, presented := identityServer(t)

	code, out, errOut := runCapture(t, "login", "--server", srv.URL, "--api-key", "vxyz_secret")
	if code != exitOK {
		t.Fatalf("login exit = %d, want 0; stderr=%s", code, errOut)
	}

	if !strings.Contains(out, "Ada Lovelace") || !strings.Contains(out, "Acme Devices") {
		t.Fatalf("login did not print the identity:\n%s", out)
	}

	if *presented != "vxyz_secret" {
		t.Fatalf("server saw key %q, want the one from --api-key", *presented)
	}

	// The config must now carry the server, the discovered org and the key.
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}

	for _, want := range []string{srv.URL, "org_01H", "vxyz_secret"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("config missing %q:\n%s", want, raw)
		}
	}

	// whoami with no flags resolves everything from the saved config.
	code, out, errOut = runCapture(t, "whoami", "--device", "d_01H")
	if code != exitOK {
		t.Fatalf("whoami exit = %d, want 0; stderr=%s", code, errOut)
	}

	for _, want := range []string{"Ada Lovelace", "admin", "Acme Devices", "rooted"} {
		if !strings.Contains(out, want) {
			t.Errorf("whoami output missing %q:\n%s", want, out)
		}
	}
}

// TestLoginRefusesWithoutACredential pins that a login with no credential fails
// before it touches the server, and writes nothing.
func TestLoginRefusesWithoutACredential(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("VYZORIX_CONFIG", configPath)
	t.Setenv("VYZORIX_API_KEY", "")
	t.Setenv("VYZORIX_DEPLOY_CREDENTIAL", "")

	code, _, _ := runCapture(t, "login", "--server", "https://unused.test")
	if code != exitUsage {
		t.Fatalf("login without credential = %d, want %d", code, exitUsage)
	}

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("a failed login wrote a config file")
	}
}

// TestLoginRefusesABadCredentialBeforeSaving pins the ordering: the server is
// asked first, so a rejected credential never leaves a broken config behind.
func TestLoginRefusesABadCredentialBeforeSaving(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("VYZORIX_CONFIG", configPath)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "api key has been revoked"})
	}))
	defer srv.Close()

	code, _, errOut := runCapture(t, "login", "--server", srv.URL, "--api-key", "vxyz_revoked")
	if code != exitRefused {
		t.Fatalf("login with a revoked key = %d, want %d; stderr=%s", code, exitRefused, errOut)
	}

	if !strings.Contains(errOut, "revoked") {
		t.Errorf("stderr did not carry the server reason: %s", errOut)
	}

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("a refused login wrote a config file")
	}
}

// TestLogoutClearsTheCredential pins that after logout a command with no flags
// has nothing to run with.
func TestLogoutClearsTheCredential(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("VYZORIX_CONFIG", configPath)
	t.Setenv("VYZORIX_SERVER", "")
	t.Setenv("VYZORIX_ORG", "")
	t.Setenv("VYZORIX_API_KEY", "")
	t.Setenv("VYZORIX_DEPLOY_CREDENTIAL", "")

	srv, _ := identityServer(t)

	if code, _, errOut := runCapture(t, "login", "--server", srv.URL, "--api-key", "vxyz_secret"); code != exitOK {
		t.Fatalf("login exit = %d; stderr=%s", code, errOut)
	}

	if code, _, errOut := runCapture(t, "logout"); code != exitOK {
		t.Fatalf("logout exit = %d; stderr=%s", code, errOut)
	}

	if code, _, _ := runCapture(t, "whoami"); code != exitUsage {
		t.Fatalf("whoami after logout = %d, want %d", code, exitUsage)
	}
}
