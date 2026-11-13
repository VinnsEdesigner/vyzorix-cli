package deploy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	vyzorixspec "github.com/VinnsEdesigner/vyzorix-spec"
)

// deviceServer serves a whoami with the given supported ABIs.
func deviceServer(t *testing.T, abis []string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(Identity{
			OperatorName: "Ada",
			Role:         "operator",
			OrgID:        "org_01H",
			Device:       &IdentityDev{ID: "d_01H", Name: "Pixel", SupportedABIs: abis},
		})
	}))
}

func TestResolveABIsRefusesARecipeAbiTheDeviceCannotRun(t *testing.T) {
	// The recipe targets arm64-v8a but the device runs x86_64: building it would
	// produce an artifact the device cannot start, so it is refused here rather
	// than discovered on the device.
	server := deviceServer(t, []string{"x86_64"})
	defer server.Close()

	client := NewClient(server.URL, "org_01H", Auth{APIKey: "vxyz_key"})
	recipe := &Recipe{Typed: vyzorixspec.BuildRecipe{
		Build:    vyzorixspec.BuildSettings{Abi: []string{"arm64-v8a"}},
		Artifact: []vyzorixspec.BuildArtifact{{App: "vfd", Name: "engine", Lang: "golang"}},
	}}

	_, err := client.resolveABIs(context.Background(), recipe, BuildOptions{Device: "d_01H"})
	if err == nil {
		t.Fatal("resolveABIs = nil error, want a refusal for an ABI the device cannot run")
	}

	if !strings.Contains(err.Error(), "arm64-v8a") {
		t.Errorf("refusal does not name the ABI: %v", err)
	}
}

func TestResolveABIsAcceptsARecipeAbiTheDeviceRuns(t *testing.T) {
	server := deviceServer(t, []string{"arm64-v8a", "armeabi-v7a"})
	defer server.Close()

	client := NewClient(server.URL, "org_01H", Auth{APIKey: "vxyz_key"})
	recipe := &Recipe{Typed: vyzorixspec.BuildRecipe{
		Build:    vyzorixspec.BuildSettings{Abi: []string{"arm64-v8a"}},
		Artifact: []vyzorixspec.BuildArtifact{{App: "vfd", Name: "engine", Lang: "golang"}},
	}}

	abis, err := client.resolveABIs(context.Background(), recipe, BuildOptions{Device: "d_01H"})
	if err != nil {
		t.Fatalf("resolveABIs = %v, want the recipe's abi", err)
	}

	if len(abis) != 1 || abis[0] != "arm64-v8a" {
		t.Errorf("abis = %v, want [arm64-v8a]", abis)
	}
}

func TestResolveABIsUsesTheDeviceWhenTheRecipeSetsNone(t *testing.T) {
	server := deviceServer(t, []string{"arm64-v8a", "x86_64"})
	defer server.Close()

	client := NewClient(server.URL, "org_01H", Auth{APIKey: "vxyz_key"})
	recipe := &Recipe{Typed: vyzorixspec.BuildRecipe{
		Artifact: []vyzorixspec.BuildArtifact{{App: "vfd", Name: "engine", Lang: "golang"}},
	}}

	abis, err := client.resolveABIs(context.Background(), recipe, BuildOptions{Device: "d_01H"})
	if err != nil {
		t.Fatalf("resolveABIs = %v", err)
	}

	if len(abis) != 2 {
		t.Errorf("abis = %v, want the device's two ABIs", abis)
	}
}

func TestAbisNotSupportedNamesTheMissing(t *testing.T) {
	if got := abisNotSupported([]string{"arm64-v8a"}, []string{"arm64-v8a"}); len(got) != 0 {
		t.Errorf("abisNotSupported = %v, want none", got)
	}

	got := abisNotSupported([]string{"arm64-v8a", "x86_64"}, []string{"arm64-v8a"})
	if len(got) != 1 || got[0] != "x86_64" {
		t.Errorf("abisNotSupported = %v, want [x86_64]", got)
	}
}
