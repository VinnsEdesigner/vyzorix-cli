// Package deploy implements the vyzorix deploy CLI: it validates a manifest
// against the server's published schema, uploads it, and drives the secret
// surface. The wire types come from the spec module (FR-23), so the
// CLI cannot drift from the server's shape.
package deploy

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Auth is how the CLI authenticates. Exactly one mode is used:
//
//   - APIKey is an operator key. It reaches every device the operator may
//     deploy to, and the server requires every request it carries to be signed
//     with a secret derived from the key (see sign).
//   - DeployCredential is a device-scoped CI token. It reaches one device, and
//     the server accepts it unsigned because the token itself is the authority.
type Auth struct {
	APIKey           string
	DeployCredential string
}

// Valid reports whether exactly one credential is set. Setting both would make
// the server choose, so the CLI refuses rather than guessing.
func (a Auth) Valid() bool {
	return (a.APIKey != "") != (a.DeployCredential != "")
}

// Client talks to one server on behalf of one org.
type Client struct {
	baseURL string
	org     string
	auth    Auth
	http    *http.Client
	now     func() time.Time
}

// NewClient builds a client. The base URL is the server root, without the /v1
// suffix: the version is part of each call's path.
func NewClient(baseURL, org string, auth Auth) *Client {
	return &Client{
		baseURL: baseURL,
		org:     org,
		auth:    auth,
		http:    &http.Client{Timeout: 60 * time.Second},
		now:     time.Now,
	}
}

// APIError is a refusal from the server, carrying its reason verbatim so the CLI
// can print it unchanged (US-009: "with the server's reason verbatim").
type APIError struct {
	Status  int
	Code    string
	Message string
}

// Error implements error.
func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Code)
	}

	return e.Message
}

// errorEnvelope is the server's structured error body.
type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// do sends one request and decodes a JSON response into out. A non-2xx status is
// returned as an *APIError with the server's own message, never a generic one.
func (c *Client) do(ctx context.Context, method, path string, body []byte, contentType string, out any) error {
	url := c.baseURL + path

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Organization-ID", c.org)

	if c.auth.DeployCredential != "" {
		req.Header.Set("X-Vyzorix-Deploy-Credential", c.auth.DeployCredential)
	} else {
		// The server identifies the caller by the key itself (X-API-Key), then
		// verifies the HMAC headers against the secret derived from it. Signing
		// without presenting the key leaves the server with no caller to resolve.
		req.Header.Set("X-API-Key", c.auth.APIKey)

		if err := c.sign(req, body); err != nil {
			return err
		}
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeAPIError(resp.StatusCode, payload)
	}

	if out == nil || len(payload) == 0 {
		return nil
	}

	return json.Unmarshal(payload, out)
}

// sign adds the HMAC headers an API-key request must carry. The secret is
// sha512(fullKey) hex — the same derivation the server stores, so the client
// needs no separate secret — and the signature covers method, path, nonce,
// timestamp and body, which is what the server verifies.
func (c *Client) sign(req *http.Request, body []byte) error {
	nonce, err := randomNonce()
	if err != nil {
		return err
	}

	ts := strconv.FormatInt(c.now().UnixMilli(), 10)
	path := req.URL.RequestURI()

	mac := hmac.New(sha512.New, []byte(deriveSigningSecret(c.auth.APIKey)))
	mac.Write([]byte(req.Method + "\n" + path + "\n" + nonce + "\n" + ts + "\n"))
	mac.Write(body)

	req.Header.Set("X-Vyzorix-Nonce", nonce)
	req.Header.Set("X-Vyzorix-Timestamp", ts)
	req.Header.Set("X-Vyzorix-Signature", base64.StdEncoding.EncodeToString(mac.Sum(nil)))

	return nil
}

// deriveSigningSecret mirrors the server's derivation: the hex SHA-512 of the
// full key. It is duplicated here because the derivation is the wire contract,
// not an implementation detail the server may change.
func deriveSigningSecret(fullKey string) string {
	sum := sha512.Sum512([]byte(fullKey))

	return hex.EncodeToString(sum[:])
}

// randomNonce returns a fresh nonce. It must be unique per request or the server
// refuses the request as a replay.
func randomNonce() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}

	return hex.EncodeToString(buf), nil
}

// decodeAPIError turns an error response into an *APIError, falling back to the
// raw body when it is not the structured envelope.
func decodeAPIError(status int, payload []byte) error {
	var env errorEnvelope
	if err := json.Unmarshal(payload, &env); err == nil && env.Error.Message != "" {
		return &APIError{Status: status, Code: env.Error.Code, Message: env.Error.Message}
	}

	msg := string(bytes.TrimSpace(payload))
	if msg == "" {
		msg = http.StatusText(status)
	}

	return &APIError{Status: status, Message: msg}
}
