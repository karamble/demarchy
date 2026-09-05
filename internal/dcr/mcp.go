// Copyright (c) 2015-2026 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package dcr

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// Resource URIs we subscribe to. Each is gated by the read domain of the same
// name in the agent's grant; subscribing needs no write scope and no spend
// grant, because MCP resources are read-only by construction.
const (
	ResNodeSync   = "dcrpulse://node/sync"
	ResWalletSync = "dcrpulse://wallet/sync"
	ResWalletBal  = "dcrpulse://wallet/balance"
	ResBRMessages = "dcrpulse://bisonrelay/messages"
	ResStaking    = "dcrpulse://staking/activity"
	ResLightning  = "dcrpulse://lightning/events"
)

// Client is a minimal MCP client over streamable HTTP.
//
// dcrpulse runs its handler with Stateless: true, so tools/call needs neither
// an initialize handshake nor a session id: a bare POST is enough. Responses
// come back SSE-framed ("event:" / "data:" lines) even for single calls, which
// is why every read goes through decodeMessages.
type Client struct {
	Endpoint string
	token    string
	hc       *http.Client
	nextID   atomic.Int64
}

func NewClient(endpoint, token string) *Client {
	if endpoint == "" {
		endpoint = DefaultEndpoint
	}
	return &Client{
		Endpoint: strings.TrimRight(endpoint, "/"),
		token:    token,
		// No global timeout: the same client serves both short calls and the
		// long-lived listen stream. Per-call deadlines come from the context.
		hc: &http.Client{},
	}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("mcp error %d: %s", e.Code, e.Message) }

type rpcMessage struct {
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}

func (c *Client) post(ctx context.Context, method string, params any) (*http.Response, error) {
	body := map[string]any{
		"jsonrpc": "2.0",
		"id":      c.nextID.Add(1),
		"method":  method,
	}
	if params != nil {
		body["params"] = params
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint+"/mcp", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		resp.Body.Close()
		return nil, ErrUnauthorized
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("http %d from %s", resp.StatusCode, c.Endpoint)
	}
	return resp, nil
}

// ErrUnauthorized means the token was rejected or is missing a domain.
var ErrUnauthorized = fmt.Errorf("unauthorized: token rejected by dcrpulse")

// decodeMessages walks an SSE-or-plain body and hands each JSON-RPC message to
// fn. It stops when fn returns false, the body ends, or the context is done.
func decodeMessages(r io.Reader, fn func(*rpcMessage) bool) error {
	sc := bufio.NewScanner(r)
	// Message payloads can be large (the BR ring, a wallet dashboard).
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "event:") || strings.HasPrefix(line, ":") {
			continue
		}
		line = strings.TrimPrefix(line, "data: ")
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var msg rpcMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if !fn(&msg) {
			return nil
		}
	}
	return sc.Err()
}

// first returns the first JSON-RPC response carrying a result or an error.
func (c *Client) first(ctx context.Context, method string, params any) (json.RawMessage, error) {
	resp, err := c.post(ctx, method, params)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var out json.RawMessage
	var rerr error
	err = decodeMessages(resp.Body, func(m *rpcMessage) bool {
		if m.Error != nil {
			rerr = m.Error
			return false
		}
		if m.Result != nil {
			out = m.Result
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if rerr != nil {
		return nil, rerr
	}
	if out == nil {
		return nil, fmt.Errorf("%s: no result in response", method)
	}
	return out, nil
}

type toolResult struct {
	StructuredContent json.RawMessage `json:"structuredContent"`
	Content           []struct {
		Text string `json:"text"`
	} `json:"content"`
	IsError bool `json:"isError"`
}

// Call invokes a tool and returns its structured payload with the uniform
// {"data": ...} envelope unwrapped.
func (c *Client) Call(ctx context.Context, name string, args map[string]any) (json.RawMessage, error) {
	if args == nil {
		args = map[string]any{}
	}
	raw, err := c.first(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	var tr toolResult
	if err := json.Unmarshal(raw, &tr); err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if tr.IsError {
		detail := ""
		if len(tr.Content) > 0 {
			detail = tr.Content[0].Text
		}
		return nil, fmt.Errorf("%s: %s", name, strings.TrimSpace(detail))
	}
	if tr.StructuredContent == nil {
		return nil, fmt.Errorf("%s: empty result", name)
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(tr.StructuredContent, &env); err == nil && env.Data != nil {
		return env.Data, nil
	}
	return tr.StructuredContent, nil
}

type resourceResult struct {
	Contents []struct {
		URI  string `json:"uri"`
		Text string `json:"text"`
	} `json:"contents"`
}

// ReadResource returns the current value of a resource URI.
func (c *Client) ReadResource(ctx context.Context, uri string) (json.RawMessage, error) {
	raw, err := c.first(ctx, "resources/read", map[string]any{"uri": uri})
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", uri, err)
	}
	var rr resourceResult
	if err := json.Unmarshal(raw, &rr); err != nil {
		return nil, fmt.Errorf("read %s: %w", uri, err)
	}
	if len(rr.Contents) == 0 {
		return json.RawMessage("null"), nil
	}
	return json.RawMessage(rr.Contents[0].Text), nil
}

// Capabilities is what the server reports this token may do.
type Capabilities struct {
	Agent     string   `json:"agent"`
	Domains   []string `json:"domains"`
	Resources []string `json:"resources"`
	Spend     struct {
		Granted     bool     `json:"granted"`
		PerTxDcr    float64  `json:"perTxDcr"`
		DailyDcr    float64  `json:"dailyDcr"`
		WriteScopes []string `json:"writeScopes"`
		Note        string   `json:"note"`
	} `json:"spend"`
}

func (c *Client) Capabilities(ctx context.Context) (*Capabilities, error) {
	raw, err := c.Call(ctx, "capabilities", nil)
	if err != nil {
		return nil, err
	}
	var caps Capabilities
	if err := json.Unmarshal(raw, &caps); err != nil {
		return nil, err
	}
	return &caps, nil
}

// HasDomain reports whether the token was granted a read domain.
func (c *Capabilities) HasDomain(name string) bool {
	for _, d := range c.Domains {
		if d == name {
			return true
		}
	}
	return false
}

// Listen opens a subscriptions/listen stream and calls onUpdate for every
// resource that changes, until the context is cancelled or the stream ends.
//
// This replaces the legacy resources/subscribe RPC and the GET-based SSE
// endpoint (go-sdk v1.7.0 / SEP-2575): which is why GET /mcp answers 405.
// onAck receives the subset of URIs the server agreed to honour; a URI missing
// from it is one this token's grant does not cover.
func (c *Client) Listen(ctx context.Context, uris []string, onAck func([]string), onUpdate func(uri string)) error {
	params := map[string]any{
		"notifications": map[string]any{"resourceSubscriptions": uris},
	}
	resp, err := c.post(ctx, "subscriptions/listen", params)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Close the body when the context ends so a blocked read unwinds.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			resp.Body.Close()
		case <-done:
		}
	}()

	return decodeMessages(resp.Body, func(m *rpcMessage) bool {
		switch {
		case strings.HasSuffix(m.Method, "subscriptions/acknowledged"):
			if onAck == nil {
				return true
			}
			var p struct {
				Notifications struct {
					ResourceSubscriptions []string `json:"resourceSubscriptions"`
				} `json:"notifications"`
			}
			if json.Unmarshal(m.Params, &p) == nil {
				onAck(p.Notifications.ResourceSubscriptions)
			}
		case strings.HasSuffix(m.Method, "resources/updated"):
			var p struct {
				URI string `json:"uri"`
			}
			if json.Unmarshal(m.Params, &p) == nil && p.URI != "" && onUpdate != nil {
				onUpdate(p.URI)
			}
		}
		return ctx.Err() == nil
	})
}

// Reachable does the cheapest possible liveness check.
func (c *Client) Reachable(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := c.Call(ctx, "node_status", nil)
	return err
}

// CheckReadOnly reports why a token is too strong for a bar widget, or nil if
// it can only read. Domain grants are deliberately not refused: they are
// read-only, and one agent may reasonably be shared with other read-only tools.
// Write scopes and spend grants are refused, because a widget that can move
// funds or send messages is a widget whose config file is worth stealing.
func (c *Capabilities) CheckReadOnly() error {
	var reasons []string
	if c.Spend.Granted {
		reasons = append(reasons, fmt.Sprintf(
			"it holds a spend grant (%.4f DCR per tx, %.4f daily)", c.Spend.PerTxDcr, c.Spend.DailyDcr))
	}
	if len(c.Spend.WriteScopes) > 0 {
		reasons = append(reasons, "it holds write scopes: "+strings.Join(c.Spend.WriteScopes, ", "))
	}
	if len(reasons) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(reasons, ", and "))
}

// MissingDomains lists the read domains the widget needs that a token lacks.
func (c *Capabilities) MissingDomains(want []string) []string {
	var missing []string
	for _, d := range want {
		if !c.HasDomain(d) {
			missing = append(missing, d)
		}
	}
	return missing
}
