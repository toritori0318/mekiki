// Package jev asks Jev, TypeSafe's calibrated classifier, the questions about a skill that
// a regular expression cannot decide. It is the only package in mekiki that opens a socket,
// and nothing here runs unless `mekiki lint --jev` is given.
package jev

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultBaseURL and DefaultModel are Jev's public endpoint and its rolling model alias.
const (
	DefaultBaseURL = "https://api.typesafe.ai"
	DefaultModel   = "jev-latest"
)

// keyVar is the environment variable the key is read from, as the vendor documents it. A
// file is never consulted: config.json belongs in version control and a key does not.
const keyVar = "TYPESAFE_API_KEY"

// ErrNoKey is returned by NewClientFromEnv when no key variable is set.
var ErrNoKey = errors.New("TYPESAFE_API_KEY is not set; export it, or run with --dry-run to see what would be sent")

// Question is one typed question. Only `noul` (a true/false statement answered with a
// probability) is used so far: every rule asks whether a defect is present, which needs no
// ordered scale and no confidence.
type Question struct {
	Type         string   `json:"type"`
	Instructions string   `json:"instructions"`
	Criteria     Criteria `json:"criteria"`
}

// Criteria spells out what true and false mean for a noul question.
type Criteria struct {
	True  string `json:"true"`
	False string `json:"false"`
}

// Noul builds a true/false question.
func Noul(statement, whenTrue, whenFalse string) Question {
	return Question{Type: "noul", Instructions: statement, Criteria: Criteria{whenTrue, whenFalse}}
}

// Answers is one response: the per-question answers plus what the server billed.
type Answers struct {
	Model       string
	InputTokens int
	raw         map[string]map[string]any
}

// Noul returns the probability the server gave a noul question, or false when the answer is
// absent or malformed. Absence is never read as "false": the caller counts it as not judged.
func (a Answers) Noul(id string) (float64, bool) {
	ans, ok := a.raw[id]
	if !ok {
		return 0, false
	}
	p, ok := ans["noul"].(float64)
	return p, ok
}

// Client talks to one Jev endpoint.
type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	// HTTP defaults to a client with a 30s timeout. Sleep defaults to time.Sleep; tests
	// replace it so retries do not wait.
	HTTP  *http.Client
	Sleep func(time.Duration)
	// Retries is how many times a 429, 5xx or network failure is tried again (default 3,
	// waiting 1s, 2s, 4s: the vendor asks for exponential back-off on 429 and 529).
	Retries int
}

// NewClientFromEnv reads the key (required) and an optional base URL from the environment.
func NewClientFromEnv() (*Client, error) {
	c := &Client{BaseURL: DefaultBaseURL, Model: DefaultModel, APIKey: strings.TrimSpace(os.Getenv(keyVar))}
	if c.APIKey == "" {
		return nil, ErrNoKey
	}
	if u := strings.TrimSpace(os.Getenv("TYPESAFE_BASE_URL")); u != "" {
		if err := checkBaseURL(u); err != nil {
			return nil, err
		}
		c.BaseURL = u
	}
	return c, nil
}

// checkBaseURL refuses anything but https, except on loopback. The key travels in the
// Authorization header, so plain http off the machine would send it in the clear.
func checkBaseURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("TYPESAFE_BASE_URL is not a URL: %w", err)
	}
	if u.Scheme == "https" {
		return nil
	}
	if u.Scheme == "http" {
		if ip := net.ParseIP(u.Hostname()); u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback()) {
			return nil
		}
	}
	return fmt.Errorf("TYPESAFE_BASE_URL must use https (plain http is allowed only for localhost): %s", raw)
}

// AuthError is a 401, 402 or 403: the account has to fix something, and retrying or moving
// on to the next skill would only repeat it. The key is never part of the message.
type AuthError struct{ Status int }

func (e *AuthError) Error() string {
	return fmt.Sprintf("jev refused the API key (HTTP %d); check TYPESAFE_API_KEY and the account's credit", e.Status)
}

// Ask sends one state with its questions and returns every answer. Transient failures
// (429, 5xx, 529, network) are retried with exponential backoff; auth failures are not.
func (c *Client) Ask(state any, questions map[string]Question) (Answers, error) {
	body, err := json.Marshal(map[string]any{"model": c.model(), "state": state, "questions": questions})
	if err != nil {
		return Answers{}, err
	}
	httpc := c.HTTP
	if httpc == nil {
		httpc = &http.Client{Timeout: 30 * time.Second}
	}
	sleep := c.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	retries := c.Retries
	if retries == 0 {
		retries = 3
	}

	var last error
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequest(http.MethodPost, strings.TrimRight(c.BaseURL, "/")+"/v1/systemone", bytes.NewReader(body))
		if err != nil {
			return Answers{}, err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", "application/json")

		res, err := httpc.Do(req)
		if err != nil {
			last = fmt.Errorf("jev: %w", err)
		} else {
			raw, _ := io.ReadAll(res.Body)
			res.Body.Close()
			switch {
			case res.StatusCode == http.StatusOK:
				return parse(raw)
			case res.StatusCode == 401 || res.StatusCode == 402 || res.StatusCode == 403:
				return Answers{}, &AuthError{Status: res.StatusCode}
			case res.StatusCode == 429 || res.StatusCode >= 500:
				last = fmt.Errorf("jev: HTTP %d", res.StatusCode)
			default:
				// Status only: a server that echoes the request would put skill text in a CI log.
				return Answers{}, fmt.Errorf("jev: HTTP %d", res.StatusCode)
			}
		}
		if attempt == retries {
			return Answers{}, fmt.Errorf("%w (gave up after %d attempts)", last, attempt+1)
		}
		sleep(time.Duration(1<<attempt) * time.Second)
	}
}

func (c *Client) model() string {
	if c.Model == "" {
		return DefaultModel
	}
	return c.Model
}

func parse(raw []byte) (Answers, error) {
	var res struct {
		Model   string                    `json:"model"`
		Answers map[string]map[string]any `json:"answers"`
		Usage   struct {
			InputTokens int `json:"input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return Answers{}, fmt.Errorf("jev: unreadable response: %w", err)
	}
	return Answers{Model: res.Model, InputTokens: res.Usage.InputTokens, raw: res.Answers}, nil
}
