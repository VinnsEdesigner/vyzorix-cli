package config

import (
	"os"
	"path/filepath"
	"testing"
)

// withTempConfig points VYZORIX_CONFIG at a file in a temp dir for the duration
// of the test, so no test touches the user's real config.
func withTempConfig(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("VYZORIX_CONFIG", path)

	return path
}

// TestLoadMissingIsEmpty pins that the state before the first login is not an
// error: a command that reads config before login must still run.
func TestLoadMissingIsEmpty(t *testing.T) {
	withTempConfig(t)

	f, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if f.Server != "" || f.Org != "" || f.APIKey != "" || f.Credential != "" {
		t.Fatalf("expected empty config, got %+v", f)
	}
}

// TestSaveLoadRoundTrip pins that what login saves is what a later command reads.
func TestSaveLoadRoundTrip(t *testing.T) {
	withTempConfig(t)

	want := &File{Server: "https://api.test", Org: "org_1", APIKey: "vxyz_key"}
	if err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if *got != *want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

// TestSaveIsOwnerOnly pins the mode: the file carries a credential, so it must
// not be group- or world-readable.
func TestSaveIsOwnerOnly(t *testing.T) {
	path := withTempConfig(t)

	if err := Save(&File{Server: "https://api.test", APIKey: "vxyz_key"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("mode = %o, want 600", perm)
	}
}

// TestRemoveIsIdempotent pins that logging out twice is not an error: the
// post-condition — no saved credential — holds either way.
func TestRemoveIsIdempotent(t *testing.T) {
	withTempConfig(t)

	if err := Save(&File{APIKey: "vxyz_key"}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := Remove(); err != nil {
		t.Fatalf("first Remove: %v", err)
	}

	if err := Remove(); err != nil {
		t.Fatalf("second Remove: %v", err)
	}

	f, err := Load()
	if err != nil {
		t.Fatalf("Load after remove: %v", err)
	}

	if f.APIKey != "" {
		t.Fatalf("credential survived logout: %+v", f)
	}
}
