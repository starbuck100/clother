package fallback

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func controlRequest(t *testing.T, base, key, action string) int {
	t.Helper()
	req, _ := http.NewRequest("POST", base+"/clother/control", strings.NewReader(`{"action":"`+action+`"}`))
	req.Header.Set("Authorization", "Bearer "+key)
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode
}
func TestSessionControlsAndRecovery(t *testing.T) {
	var exhausted atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		raw, _ := io.ReadAll(req.Body)
		if exhausted.Load() && strings.Contains(string(raw), `"model":"free"`) {
			w.WriteHeader(429)
			fmt.Fprint(w, `{"error":{"message":"quota"}}`)
			return
		}
		fmt.Fprint(w, `{"type":"message","content":[{"type":"text","text":"OK"}]}`)
	}))
	defer server.Close()
	r := &Router{Routes: []Route{route("kilo", "free", server.URL, true), route("deepseek", "paid", server.URL, false)}, RecoveryInterval: time.Nanosecond}
	base, key := start(t, r)
	if controlRequest(t, base, "wrong", "next") != 401 {
		t.Fatal("unauthorized control")
	}
	controlRequest(t, base, key, "pin")
	exhausted.Store(true)
	if code, _ := request(t, base, key, false); code != 429 {
		t.Fatal("pinned route switched")
	}
	controlRequest(t, base, key, "auto")
	if code, _ := request(t, base, key, false); code != 200 || r.State().Free {
		t.Fatal("paid fallback failed")
	}
	// A timer or a failed probe must never be enough to return to free.
	var probes atomic.Int32
	r.Recover = func(context.Context, Route) bool { probes.Add(1); return false }
	request(t, base, key, false)
	if r.State().Free || probes.Load() != 1 {
		t.Fatal("unconfirmed return")
	}
	exhausted.Store(false)
	r.Recover = func(context.Context, Route) bool { return true }
	request(t, base, key, false)
	if !r.State().Free || r.State().Reason != "free availability confirmed by probe" {
		t.Fatal("confirmed return missing")
	}
	controlRequest(t, base, key, "free")
	exhausted.Store(true)
	if code, _ := request(t, base, key, false); code != 429 {
		t.Fatal("free-only allowed paid request")
	}
}
func TestControlDoesNotInterruptInflightStream(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if calls.Add(1) == 1 {
			close(entered)
			<-release
		}
		fmt.Fprint(w, `{"type":"message","content":[{"type":"text","text":"OK"}]}`)
	}))
	defer server.Close()
	r := &Router{Routes: []Route{route("kilo", "first", server.URL, true), route("kilo", "second", server.URL, true)}}
	base, key := start(t, r)
	finished := make(chan struct{})
	go func() { defer close(finished); request(t, base, key, false) }()
	<-entered
	if controlRequest(t, base, key, "next") != 200 || r.State().Model != "first" {
		t.Fatal("control mutated in-flight route")
	}
	close(release)
	<-finished
	request(t, base, key, false)
	if r.State().Model != "second" {
		t.Fatal("next did not apply at boundary")
	}
}
func TestEmpiricalRankingPreservesFreeFirst(t *testing.T) {
	r := &Router{Routes: []Route{route("kilo", "original", "", true), route("kilo", "unreliable", "", true), route("kilo", "reliable", "", true), route("paid", "fast", "", false)}, Rank: func(r Route) float64 {
		if r.Model == "reliable" {
			return 5
		}
		return 0
	}}
	if got := r.next(map[int]bool{0: true}, map[string]bool{}, 100, false); got != 2 {
		t.Fatalf("next %d", got)
	}
}
