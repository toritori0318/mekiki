package jev

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The client is the only thing in mekiki that opens a socket, so its contract is pinned
// tightly: what it sends, what it reads back, and what it does when the server says no.

func TestAskSendsTheRequestJevExpectsAndReadsTheProbability(t *testing.T) {
	var got struct {
		Model     string                    `json:"model"`
		State     map[string]any            `json:"state"`
		Questions map[string]map[string]any `json:"questions"`
	}
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"model":"jev-1.13.0","answers":{"q1":{"type":"noul","noul":0.83}},"usage":{"input_tokens":120,"output_tokens":3}}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIKey: "k-secret", Model: "jev-latest"}
	ans, err := c.Ask(map[string]any{"description": "x"}, map[string]Question{
		"q1": Noul("The trigger contradicts the description.", "yes", "no"),
	})
	if err != nil {
		t.Fatal(err)
	}

	if auth != "Bearer k-secret" {
		t.Errorf("Authorization = %q", auth)
	}
	if got.Model != "jev-latest" || got.State["description"] != "x" {
		t.Errorf("body = %+v", got)
	}
	q := got.Questions["q1"]
	if q["type"] != "noul" || q["instructions"] != "The trigger contradicts the description." {
		t.Errorf("question = %+v", q)
	}
	if crit, _ := q["criteria"].(map[string]any); crit["true"] != "yes" || crit["false"] != "no" {
		t.Errorf("criteria = %+v", q["criteria"])
	}
	if p, ok := ans.Noul("q1"); !ok || p != 0.83 {
		t.Errorf("Noul(q1) = %v, %v", p, ok)
	}
	if ans.InputTokens != 120 {
		t.Errorf("InputTokens = %d", ans.InputTokens)
	}
}

func TestAskDoesNotRetryARefusedKeyAndNeverPrintsIt(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(401)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIKey: "k-secret", Sleep: func(time.Duration) {}}
	_, err := c.Ask("s", map[string]Question{"q": Noul("x", "y", "n")})

	var ae *AuthError
	if !errors.As(err, &ae) || ae.Status != 401 {
		t.Fatalf("err = %v, want AuthError 401", err)
	}
	if hits != 1 {
		t.Errorf("hits = %d, want a single attempt: retrying a bad key only repeats it", hits)
	}
	if strings.Contains(err.Error(), "k-secret") {
		t.Errorf("error leaks the key: %q", err)
	}
}

func TestAskRetriesARateLimitAndSucceeds(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			w.WriteHeader(429)
			return
		}
		w.Write([]byte(`{"answers":{"q":{"type":"noul","noul":0.1}}}`))
	}))
	defer srv.Close()

	var slept []time.Duration
	c := &Client{BaseURL: srv.URL, APIKey: "k", Sleep: func(d time.Duration) { slept = append(slept, d) }}
	ans, err := c.Ask("s", map[string]Question{"q": Noul("x", "y", "n")})
	if err != nil {
		t.Fatal(err)
	}
	if p, _ := ans.Noul("q"); p != 0.1 || hits != 2 || len(slept) != 1 {
		t.Errorf("noul=%v hits=%d slept=%v, want one back-off then the answer", p, hits, slept)
	}
}

func TestAskGivesUpAfterTheRetryBudget(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(500)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, APIKey: "k", Retries: 2, Sleep: func(time.Duration) {}}
	_, err := c.Ask("s", map[string]Question{"q": Noul("x", "y", "n")})

	if err == nil || hits != 3 || !strings.Contains(err.Error(), "500") {
		t.Errorf("err=%v hits=%d, want 3 attempts and an error naming the status", err, hits)
	}
}

func TestNoulIsAbsentNotFalseWhenTheServerLeftItOut(t *testing.T) {
	// A missing answer must be counted as "not judged" by the caller; reading it as 0 would
	// turn a lost verdict into a clean one.
	ans, _ := parse([]byte(`{"answers":{"other":{"type":"noul","noul":0.9}}}`))
	if _, ok := ans.Noul("q"); ok {
		t.Error("Noul reported an answer the server never gave")
	}
}
