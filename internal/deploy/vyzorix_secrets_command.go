package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/VinnsEdesigner/vyzorix-cli/internal/ui"
)

// The secret surface (§9b.6). A value enters on `set` and leaves on no CLI call:
// the device fetches its own ciphertext, wrapped to its key. `list` shows names
// and generations, which is what an operator needs to confirm the right thing is
// set.

// setSecretRequest is the POST body the server expects.
type setSecretRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// secretName is one secret's name and generation, never its value.
type secretName struct {
	Key        string `json:"key"`
	Generation int64  `json:"generation"`
}

// secretListResponse is the list envelope.
type secretListResponse struct {
	Secrets []secretName `json:"secrets"`
}

// SetSecret stores one secret value, wrapped to the device's key server-side.
func (c *Client) SetSecret(ctx context.Context, device, app, key, value string) error {
	body, err := json.Marshal(setSecretRequest{Key: key, Value: value})
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/v1/deploy/%s/secrets/%s", url.PathEscape(device), url.PathEscape(app))

	return c.do(ctx, http.MethodPost, path, body, "application/json", nil)
}

// ListSecrets prints an app's secret names and generations. It never returns a
// value, because the server never sends one.
func (c *Client) ListSecrets(ctx context.Context, device, app string, p *ui.Printer) error {
	var resp secretListResponse

	path := fmt.Sprintf("/v1/deploy/%s/secrets/%s", url.PathEscape(device), url.PathEscape(app))
	if err := c.do(ctx, http.MethodGet, path, nil, "", &resp); err != nil {
		return err
	}

	// Sort for a stable listing; the store's order is not part of the contract.
	sort.Slice(resp.Secrets, func(i, j int) bool { return resp.Secrets[i].Key < resp.Secrets[j].Key })

	if len(resp.Secrets) == 0 {
		p.Detail("%s: no secrets set", app)

		return nil
	}

	rows := make([][]string, 0, len(resp.Secrets))
	for _, secret := range resp.Secrets {
		rows = append(rows, []string{secret.Key, fmt.Sprintf("%d", secret.Generation)})
	}

	p.Table([]string{"KEY", "GENERATION"}, rows)

	return nil
}

// UnsetSecret removes one secret.
func (c *Client) UnsetSecret(ctx context.Context, device, app, key string) error {
	path := fmt.Sprintf("/v1/deploy/%s/secrets/%s/%s",
		url.PathEscape(device), url.PathEscape(app), url.PathEscape(key))

	return c.do(ctx, http.MethodDelete, path, nil, "", nil)
}

// SplitAssignment splits a KEY=VALUE argument. The value may contain '='; only
// the first one separates, which is what lets a URL or a base64 blob be a value.
func SplitAssignment(arg string) (string, string, error) {
	key, value, ok := strings.Cut(arg, "=")
	if !ok || key == "" {
		return "", "", fmt.Errorf("expected KEY=VALUE, got %q", arg)
	}

	return key, value, nil
}
