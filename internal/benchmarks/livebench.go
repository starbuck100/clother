// Package benchmarks reads public benchmark evidence, never a fixed model shortlist.
package benchmarks

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Row struct {
	Model      string             `json:"model"`
	Categories map[string]float64 `json:"categories"`
}
type Report struct {
	ModelsMeasured int       `json:"models_measured"`
	Categories     []string  `json:"categories,omitempty"`
	Source         string    `json:"source"`
	Dataset        string    `json:"dataset"`
	Modified       string    `json:"source_last_modified,omitempty"`
	Fetched        time.Time `json:"fetched_at"`
	Stale          bool      `json:"stale"`
	Error          string    `json:"error,omitempty"`
	Rows           []Row     `json:"rows,omitempty"`
}

var scriptRE = regexp.MustCompile(`(?:\./|/)?static/js/main\.[a-zA-Z0-9]+\.js`)
var versionsRE = regexp.MustCompile(`\["\d{4}-\d{2}-\d{2}"(?:,"\d{4}-\d{2}-\d{2}"){3,}\]`)

// Load discovers the current dataset from the public website. A failed refresh
// never makes an old snapshot appear current. No extra API key is required.
func Load(ctx context.Context, cache string) Report {
	path := filepath.Join(cache, "livebench.json")
	var old Report
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &old)
	}
	if len(old.Rows) > 0 && time.Since(old.Fetched) < time.Hour {
		return old
	}
	fresh, err := Fetch(ctx, "https://livebench.ai")
	if err != nil {
		old.Stale = true
		old.Error = err.Error()
		old.Source = "https://livebench.ai/"
		return old
	}
	if data, err := json.Marshal(fresh); err == nil {
		_ = os.MkdirAll(cache, 0700)
		_ = os.WriteFile(path, data, 0600)
	}
	return fresh
}
func Fetch(ctx context.Context, base string) (Report, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	get := func(path string) ([]byte, string, error) {
		req, err := http.NewRequestWithContext(ctx, "GET", base+path, nil)
		if err != nil {
			return nil, "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, "", fmt.Errorf("LiveBench unavailable")
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			return nil, "", fmt.Errorf("LiveBench HTTP %d", resp.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		return data, resp.Header.Get("Last-Modified"), err
	}
	var r Report
	page, _, err := get("/")
	if err != nil {
		return r, err
	}
	script := scriptRE.FindString(string(page))
	if script == "" {
		return r, fmt.Errorf("LiveBench website format changed")
	}
	js, _, err := get("/" + strings.TrimPrefix(strings.TrimPrefix(script, "./"), "/"))
	if err != nil {
		return r, err
	}
	var dates []string
	if err = json.Unmarshal([]byte(versionsRE.FindString(string(js))), &dates); err != nil {
		return r, fmt.Errorf("LiveBench dataset discovery failed")
	}
	sort.Strings(dates)
	version := dates[len(dates)-1]
	suffix := strings.ReplaceAll(version, "-", "_")
	data, modified, err := get("/table_" + suffix + ".csv")
	if err != nil {
		return r, err
	}
	categories, _, err := get("/categories_" + suffix + ".json")
	if err != nil {
		return r, err
	}
	rows, err := Parse(data, categories)
	if err != nil {
		return r, err
	}
	names := map[string]bool{}
	for _, row := range rows {
		for name := range row.Categories {
			names[name] = true
		}
	}
	var categoryNames []string
	for name := range names {
		categoryNames = append(categoryNames, name)
	}
	sort.Strings(categoryNames)
	return Report{Source: base + "/", Dataset: version, Modified: modified, Fetched: time.Now().UTC(), Rows: rows, ModelsMeasured: len(rows), Categories: categoryNames}, nil
}
func Parse(data, categories []byte) ([]Row, error) {
	var groups map[string][]string
	if err := json.Unmarshal(categories, &groups); err != nil {
		return nil, err
	}
	records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil || len(records) < 2 {
		return nil, fmt.Errorf("invalid LiveBench table")
	}
	columns := map[string]int{}
	for i, h := range records[0] {
		columns[h] = i
	}
	if _, ok := columns["model"]; !ok {
		return nil, fmt.Errorf("missing benchmark model column")
	}
	var rows []Row
	for _, record := range records[1:] {
		row := Row{Model: record[columns["model"]], Categories: map[string]float64{}}
		for group, tasks := range groups {
			sum := 0.0
			valid := len(tasks) > 0
			for _, task := range tasks {
				i, ok := columns[task]
				if !ok {
					valid = false
					break
				}
				n, err := strconv.ParseFloat(record[i], 64)
				if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 100 {
					valid = false
					break
				}
				sum += n
			}
			if valid {
				row.Categories[group] = sum / float64(len(tasks))
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}
func (r Report) Match(id string) []Row {
	id = strings.TrimSuffix(id, ":free")
	if i := strings.LastIndex(id, "/"); i >= 0 {
		id = id[i+1:]
	}
	var out []Row
	for _, row := range r.Rows {
		if strings.EqualFold(row.Model, id) {
			out = append(out, row)
		}
	}
	return out // Different versions/effort settings and stealth identities are NOT aliases.
}
