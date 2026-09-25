// Package usage records response metadata only: never prompts, outputs or keys.
package usage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jolehuit/clother/internal/platform"
)

type Limit struct {
	Kind        string     `json:"kind"`
	Scope       string     `json:"scope"` // provider, model, or unknown
	Ceiling     *int       `json:"limit,omitempty"`
	Remaining   *int       `json:"remaining,omitempty"`
	ResetAt     *time.Time `json:"reset_at,omitempty"`
	ResetSource string     `json:"reset_source,omitempty"`
}

type Event struct {
	At         time.Time `json:"at"`
	Provider   string    `json:"provider"`
	Scope      string    `json:"account_scope"`
	Model      string    `json:"model"`
	Free       bool      `json:"free"`
	Images     bool      `json:"images"`
	Status     int       `json:"http_status"`
	Outcome    string    `json:"outcome"`
	Input      *int      `json:"input_tokens,omitempty"`
	Output     *int      `json:"output_tokens,omitempty"`
	CacheRead  *int      `json:"cache_read_tokens,omitempty"`
	CacheWrite *int      `json:"cache_write_tokens,omitempty"`
	Cost       *float64  `json:"cost_usd,omitempty"`
	Limit      *Limit    `json:"limit,omitempty"`
	DurationMS int64     `json:"duration_ms"`
}

// OpenRouter quotas belong to credentials; Kilo free quotas are shared by IP
// across credentials. The latter can only be approximated by local endpoint
// traffic: other devices/IP changes cannot be inferred from this ledger.
func Scope(provider, base, key string) string {
	if provider == "kilo" {
		key = ""
	}
	hash := sha256.Sum256([]byte(provider + "\n" + strings.TrimRight(base, "/") + "\n" + key))
	return hex.EncodeToString(hash[:16])
}

type Store struct{ Dir string }

func NewStore(dataDir string) Store { return Store{Dir: filepath.Join(dataDir, "usage", "v1")} }

func (s Store) Record(event Event) error {
	if s.Dir == "" {
		return nil
	}
	dir := filepath.Join(s.Dir, event.At.UTC().Format("2006-01-02"))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	// One immutable file per request avoids lost updates between processes. A
	// complete atomic rename also keeps readers from seeing half-written JSON.
	return platform.AtomicWrite(filepath.Join(dir, hex.EncodeToString(id)+".json"), data, 0600)
}

func (s Store) Read(now time.Time) ([]Event, error) {
	var events []Event
	// The usage view deliberately covers only the last 30 days. Older metadata
	// stays on disk until the user removes the usage directory.
	for day := 0; day < 31; day++ {
		dir := filepath.Join(s.Dir, now.UTC().AddDate(0, 0, -day).Format("2006-01-02"))
		files, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, file.Name()))
			if err != nil {
				return nil, err
			}
			var event Event
			if len(data) > 16384 || json.Unmarshal(data, &event) != nil {
				return nil, fmt.Errorf("invalid local usage record %s", file.Name())
			}
			if event.At.After(now) || event.At.Before(now.Add(-30*24*time.Hour)) {
				continue
			}
			events = append(events, event)
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
	return events, nil
}

type Totals struct {
	Attempts     int     `json:"attempts"`
	Succeeded    int     `json:"succeeded"`
	Failed       int     `json:"failed_or_incomplete"`
	Measured     int     `json:"requests_with_token_usage"`
	Input        int     `json:"input_tokens"`
	Output       int     `json:"output_tokens"`
	CacheRead    int     `json:"cache_read_tokens"`
	CacheWrite   int     `json:"cache_write_tokens"`
	KnownCost    float64 `json:"reported_cost_usd"`
	CostMeasured int     `json:"requests_with_cost"`
}

func Sum(events []Event, since time.Time) Totals {
	var total Totals
	for _, e := range events {
		if e.At.Before(since) {
			continue
		}
		total.Attempts++
		if e.Outcome == "ok" {
			total.Succeeded++
		} else {
			total.Failed++
		}
		if e.Input != nil || e.Output != nil {
			total.Measured++
		}
		if e.Input != nil {
			total.Input += *e.Input
		}
		if e.Output != nil {
			total.Output += *e.Output
		}
		if e.CacheRead != nil {
			total.CacheRead += *e.CacheRead
		}
		if e.CacheWrite != nil {
			total.CacheWrite += *e.CacheWrite
		}
		if e.Cost != nil {
			total.KnownCost += *e.Cost
			total.CostMeasured++
		}
	}
	return total
}

// ActiveBlocks keeps provider-wide and model-specific errors distinct. A later
// successful request clears the matching observation. Without a reported reset,
// short rate limits become recheckable after an hour rather than blocking forever.
func ActiveBlocks(events []Event, now time.Time) []Event {
	var blocks []Event
	for i, e := range events {
		if e.Limit == nil || e.Limit.Kind == "" {
			continue
		}
		if e.Limit.ResetAt != nil && !now.Before(*e.Limit.ResetAt) {
			continue
		}
		if e.Limit.ResetAt == nil && now.Sub(e.At) > time.Hour {
			continue
		}
		cleared := false
		for _, later := range events[i+1:] {
			if later.Outcome == "ok" && (e.Limit.Scope == "provider" || later.Model == e.Model) {
				cleared = true
				break
			}
		}
		if !cleared {
			blocks = append(blocks, e)
		}
	}
	return blocks
}
