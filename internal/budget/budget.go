// Package budget reserves conservative costs before paid fallback inference.
// Amounts are local upper estimates, not a claim about the provider's invoice.
package budget

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/jolehuit/clother/internal/platform"
	"github.com/jolehuit/clother/internal/usage"
)

type Price struct {
	Input   float64   `json:"input_usd_per_million"`
	Output  float64   `json:"output_usd_per_million"`
	Expires time.Time `json:"expires_at"`
	Source  string    `json:"source"`
}
type Config struct {
	Daily   float64          `json:"daily_usd"`
	Session float64          `json:"session_usd"`
	Prices  map[string]Price `json:"prices,omitempty"` // provider/model; explicit endpoint-specific rates
}

func Defaults(c *Config) Config {
	if c == nil {
		return Config{Daily: 1, Session: 1}
	}
	return *c
}
func Valid(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1000000 }
func (p Price) Valid(now time.Time) bool {
	return Valid(p.Input) && Valid(p.Output) && p.Input > 0 && p.Output > 0 && now.Before(p.Expires)
}
func amount(v float64) int64 { return int64(math.Ceil(v * 1e9)) }

type Entry struct {
	ID      string    `json:"id"`
	Session string    `json:"session"`
	At      time.Time `json:"at"`
	Nano    int64     `json:"nanodollars"`
	Pending bool      `json:"pending"`
}
type Ledger struct {
	Dir, Session string
	Config       Config
}
type Summary struct {
	Daily        float64 `json:"daily_estimate_and_reservations_usd"`
	Session      float64 `json:"session_estimate_and_reservations_usd"`
	DailyLimit   float64 `json:"daily_limit_usd"`
	SessionLimit float64 `json:"session_limit_usd"`
}

func ID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return ""
	}
	return hex.EncodeToString(b)
}
func (l Ledger) read() ([]Entry, error) {
	data, e := os.ReadFile(filepath.Join(l.Dir, "ledger.json"))
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var entries []Entry
	e = json.Unmarshal(data, &entries)
	for _, v := range entries {
		if v.Nano < 0 {
			return nil, fmt.Errorf("invalid budget ledger")
		}
	}
	return entries, e
}
func totals(entries []Entry, session string, now time.Time) (int64, int64) {
	var day, total int64
	for _, e := range entries {
		if e.Pending || e.At.UTC().Format("2006-01-02") == now.UTC().Format("2006-01-02") {
			day += e.Nano
		}
		if e.Session == session {
			total += e.Nano
		}
	}
	return day, total
}
func (l Ledger) Summary() (Summary, error) {
	entries, e := l.read()
	d, s := totals(entries, l.Session, time.Now())
	return Summary{float64(d) / 1e9, float64(s) / 1e9, l.Config.Daily, l.Config.Session}, e
}

// A directory lock works across Windows and Unix processes. An abandoned lock
// fails closed; it is never guessed stale while another process may own it.
func (l Ledger) update(fn func(*[]Entry) error) error {
	if e := os.MkdirAll(l.Dir, 0700); e != nil {
		return e
	}
	lock := filepath.Join(l.Dir, "lock")
	deadline := time.Now().Add(2 * time.Second)
	for {
		e := os.Mkdir(lock, 0700)
		if e == nil {
			break
		}
		if !os.IsExist(e) || time.Now().After(deadline) {
			return fmt.Errorf("budget ledger locked or unavailable; paid request blocked")
		}
		time.Sleep(10 * time.Millisecond)
	}
	defer os.Remove(lock)
	entries, e := l.read()
	if e != nil {
		return e
	}
	if e = fn(&entries); e != nil {
		return e
	}
	data, e := json.Marshal(entries)
	if e != nil {
		return e
	}
	return platform.AtomicWrite(filepath.Join(l.Dir, "ledger.json"), data, 0600)
}
func (l Ledger) Reserve(price Price, inputBound, outputBound int) (func(usage.Event), error) {
	now := time.Now().UTC()
	if l.Session == "" || !Valid(l.Config.Daily) || !Valid(l.Config.Session) || !price.Valid(now) || inputBound <= 0 || outputBound <= 0 {
		return nil, fmt.Errorf("paid fallback blocked: valid current price and budget required")
	}
	reserved := amount((float64(inputBound)*price.Input + float64(outputBound)*price.Output) / 1e6)
	id := ID()
	if id == "" {
		return nil, fmt.Errorf("budget reservation unavailable")
	}
	e := l.update(func(entries *[]Entry) error {
		d, s := totals(*entries, l.Session, now)
		if reserved > amount(l.Config.Daily)-d || reserved > amount(l.Config.Session)-s {
			return fmt.Errorf("paid fallback blocked: daily/session budget exhausted (reservation $%.6f)", float64(reserved)/1e9)
		}
		*entries = append(*entries, Entry{id, l.Session, now, reserved, true})
		return nil
	})
	if e != nil {
		return nil, e
	}
	return func(event usage.Event) {
		spent := reserved
		if event.Outcome == "ok" && event.Input != nil && event.Output != nil {
			input := *event.Input
			if event.CacheRead != nil {
				input += *event.CacheRead
			}
			if event.CacheWrite != nil {
				input += *event.CacheWrite
			}
			spent = amount((float64(input)*price.Input + float64(*event.Output)*price.Output) / 1e6)
		} else if event.Status == 400 || event.Status == 401 || event.Status == 403 || event.Status == 429 {
			spent = 0
		}
		if event.Cost != nil && Valid(*event.Cost) {
			spent = max(spent, amount(*event.Cost))
		}
		// Unknown completion retains the full reservation, including after a crash.
		_ = l.update(func(entries *[]Entry) error {
			for i := range *entries {
				if (*entries)[i].ID == id {
					(*entries)[i].Nano = spent
					(*entries)[i].Pending = false
					(*entries)[i].At = time.Now().UTC()
					return nil
				}
			}
			return fmt.Errorf("missing reservation")
		})
	}, nil
}
