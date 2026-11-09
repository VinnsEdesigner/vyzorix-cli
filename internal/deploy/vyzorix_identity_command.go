package deploy

import (
	"context"
	"net/http"
	"net/url"

	"github.com/VinnsEdesigner/vyzorix-cli/internal/ui"
)

// Identity is who the credential authenticates as, as the server reports it. The
// device half is present only when a device was named.
type Identity struct {
	OperatorName string       `json:"operatorName"`
	Email        string       `json:"email"`
	Role         string       `json:"role"`
	OrgID        string       `json:"orgId"`
	OrgName      string       `json:"orgName"`
	Device       *IdentityDev `json:"device,omitempty"`
}

// IdentityDev is the device half of an Identity.
type IdentityDev struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Tier string `json:"tier"`
}

// Whoami resolves the credential's identity, optionally scoped to a device so its
// tier is reported. It is the call `vyzorix login` makes to prove a credential
// works before it is saved.
func (c *Client) Whoami(ctx context.Context, device string) (*Identity, error) {
	path := "/v1/deploy/whoami"
	if device != "" {
		path += "?device=" + url.QueryEscape(device)
	}

	var identity Identity
	if err := c.do(ctx, http.MethodGet, path, nil, "", &identity); err != nil {
		return nil, err
	}

	return &identity, nil
}

// PrintIdentity renders an identity: the operator, the org, and the device when
// one was named.
func PrintIdentity(p *ui.Printer, id *Identity) {
	name := id.OperatorName
	if name == "" {
		name = "(unnamed operator)"
	}

	role := id.Role
	if role == "" {
		role = "unknown"
	}

	org := id.OrgName
	if org == "" {
		org = id.OrgID
	}

	fields := [][2]string{
		{"user", name},
		{"role", role},
		{"org", org},
	}

	if id.Email != "" {
		fields = append(fields, [2]string{"email", id.Email})
	}

	p.Fields(fields)

	if id.Device != nil {
		p.Blank()

		device := id.Device.ID
		if id.Device.Name != "" {
			device = id.Device.Name + " (" + id.Device.ID + ")"
		}

		p.Table([]string{"DEVICE", "TIER"}, [][]string{{device, id.Device.Tier}})
	}
}
