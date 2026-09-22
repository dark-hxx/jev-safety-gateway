// Package jev is a client for the TypeSafe evaluation endpoint
// (POST {base}/v1/systemone). It rotates over a set of API keys round-robin so
// no single key hits its rate limit, and retries the next key on 401/429/529.
package jev

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

// KeyProvider supplies the currently-enabled JEV keys and records usage.
type KeyProvider interface {
	// EnabledKeys returns id/value pairs of enabled keys in stable order.
	EnabledKeys() ([]Key, error)
	// MarkKeyUsed records that a key handled a request.
	MarkKeyUsed(id int64)
}

// Key is a single JEV API key.
type Key struct {
	ID  int64
	Key string
}

// Client talks to the JEV evaluation endpoint.
type Client struct {
	http *http.Client
	keys KeyProvider
	rr   uint64 // round-robin cursor
}

// New creates a client. The http client's timeout is set per-request via ctx.
func New(keys KeyProvider) *Client {
	return &Client{
		http: &http.Client{},
		keys: keys,
	}
}

// EvalRequest is the request body for the noul evaluation.
type evalRequest struct {
	State     string              `json:"state"`
	Model     string              `json:"model"`
	Questions map[string]question `json:"questions"`
}

type question struct {
	Type         string            `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
}

type evalResponse struct {
	Model   string `json:"model"`
	Answers map[string]struct {
		Type string  `json:"type"`
		Noul float64 `json:"noul"`
	} `json:"answers"`
}

// Params configures one evaluation.
type Params struct {
	BaseURL     string
	Model       string
	State       string
	Instruction string
	TimeoutMS   int
}

// Score returns the noul probability in [0,1] that the content is SAFE
// (per the instruction). It rotates keys and retries transient failures.
func (c *Client) Score(ctx context.Context, p Params) (float64, error) {
	keys, err := c.keys.EnabledKeys()
	if err != nil {
		return 0, fmt.Errorf("load keys: %w", err)
	}
	if len(keys) == 0 {
		return 0, fmt.Errorf("no enabled JEV keys configured")
	}

	body, err := json.Marshal(evalRequest{
		State: p.State,
		Model: p.Model,
		Questions: map[string]question{
			"safety": {
				Type:         "noul",
				Instructions: p.Instruction,
				Criteria: map[string]string{
					"true":  "safe to process",
					"false": "harmful or disallowed",
				},
			},
		},
	})
	if err != nil {
		return 0, err
	}

	url := trimSlash(p.BaseURL) + "/v1/systemone"
	timeout := time.Duration(p.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 8 * time.Second
	}

	// Start at the round-robin cursor and try each key once.
	start := int(atomic.AddUint64(&c.rr, 1))
	var lastErr error
	for i := 0; i < len(keys); i++ {
		k := keys[(start+i)%len(keys)]
		score, retry, err := c.callOnce(ctx, url, k, body, timeout)
		if err == nil {
			c.keys.MarkKeyUsed(k.ID)
			return score, nil
		}
		lastErr = err
		if !retry {
			return 0, err
		}
	}
	return 0, fmt.Errorf("all JEV keys failed: %w", lastErr)
}

// callOnce performs a single request. retry indicates the failure is transient
// or key-specific and the next key should be tried.
func (c *Client) callOnce(ctx context.Context, url string, k Key, body []byte, timeout time.Duration) (score float64, retry bool, err error) {
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+k.Key)

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, true, err // network/timeout: try next key
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch {
	case resp.StatusCode == http.StatusOK:
		var er evalResponse
		if err := json.Unmarshal(data, &er); err != nil {
			return 0, false, fmt.Errorf("decode JEV response: %w", err)
		}
		ans, ok := er.Answers["safety"]
		if !ok {
			return 0, false, fmt.Errorf("JEV response missing 'safety' answer")
		}
		return ans.Noul, false, nil
	case resp.StatusCode == http.StatusUnauthorized,
		resp.StatusCode == http.StatusTooManyRequests,
		resp.StatusCode == 529:
		// key-specific or transient: rotate to next key
		return 0, true, fmt.Errorf("JEV status %d: %s", resp.StatusCode, snippet(data))
	default:
		return 0, false, fmt.Errorf("JEV status %d: %s", resp.StatusCode, snippet(data))
	}
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}

func snippet(b []byte) string {
	if len(b) > 300 {
		return string(b[:300])
	}
	return string(b)
}
