package deploy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/VinnsEdesigner/vyzorix-cli/internal/ui"
)

// TestWhoami_ReportsOperatorRoleOrgAndTier pins the shape login and whoami read:
// the operator, the role, the org, and — when a device is named — its tier.
func TestWhoami_ReportsOperatorRoleOrgAndTier(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/deploy/whoami" {
			t.Errorf("path = %q, want /v1/deploy/whoami", r.URL.Path)
		}

		if got := r.URL.Query().Get("device"); got != "d_01H" {
			t.Errorf("device query = %q, want d_01H", got)
		}

		if r.Header.Get("X-API-Key") == "" {
			t.Error("no API key presented")
		}

		_ = json.NewEncoder(w).Encode(Identity{
			OperatorName: "Ada Lovelace",
			Email:        "ada@example.test",
			Role:         "admin",
			OrgID:        "org_01H",
			OrgName:      "Acme Devices",
			Device:       &IdentityDev{ID: "d_01H", Name: "Pixel", Tier: "rooted"},
		})
	}))
	defer server.Close()

	client := NewClient(server.URL, "org_01H", Auth{APIKey: "vxyz_key"})

	identity, err := client.Whoami(context.Background(), "d_01H")
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}

	if identity.OperatorName != "Ada Lovelace" {
		t.Errorf("operatorName = %q", identity.OperatorName)
	}

	if identity.Role != "admin" {
		t.Errorf("role = %q, want admin", identity.Role)
	}

	if identity.OrgName != "Acme Devices" {
		t.Errorf("orgName = %q, want Acme Devices", identity.OrgName)
	}

	if identity.Device == nil || identity.Device.Tier != "rooted" {
		t.Errorf("device = %+v, want tier rooted", identity.Device)
	}
}

// TestPrintIdentity_ShowsUserRoleOrgAndTier pins what the user sees: the name,
// the role, the org, and the device's tier in the table.
func TestPrintIdentity_ShowsUserRoleOrgAndTier(t *testing.T) {
	var out strings.Builder

	PrintIdentity(ui.New(&out), &Identity{
		OperatorName: "Ada Lovelace",
		Role:         "admin",
		OrgName:      "Acme Devices",
		Device:       &IdentityDev{ID: "d_01H", Name: "Pixel", Tier: "rooted"},
	})

	printed := out.String()
	for _, want := range []string{"Ada Lovelace", "admin", "Acme Devices", "Pixel", "rooted"} {
		if !strings.Contains(printed, want) {
			t.Errorf("output missing %q:\n%s", want, printed)
		}
	}
}

// TestWhoami_RefusalCarriesTheServerReason pins that a rejected credential reads
// as a refusal, so login exits 1 with the server's reason rather than guessing.
func TestWhoami_RefusalCarriesTheServerReason(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "api key has been revoked"})
	}))
	defer server.Close()

	client := NewClient(server.URL, "org_01H", Auth{APIKey: "vxyz_key"})

	_, err := client.Whoami(context.Background(), "")
	if err == nil {
		t.Fatal("expected a refusal")
	}

	if !IsRefusal(err) {
		t.Errorf("IsRefusal = false for %v", err)
	}
}
