package budget

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const DeepSeekPricingURL = "https://api-docs.deepseek.com/quick_start/pricing/"

// Fetch each session; never silently use stale or third-party gateway prices
// for the direct DeepSeek endpoint. Changed table layouts fail closed.
func DeepSeek(ctx context.Context) (Price, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, e := http.NewRequestWithContext(ctx, "GET", DeepSeekPricingURL, nil)
	if e != nil {
		return Price{}, e
	}
	client := http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, e := client.Do(req)
	if e != nil {
		return Price{}, e
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Price{}, fmt.Errorf("pricing unavailable")
	}
	data, e := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if e != nil {
		return Price{}, e
	}
	return ParseDeepSeek(string(data), time.Now())
}
func ParseDeepSeek(page string, now time.Time) (Price, error) {
	table := regexp.MustCompile(`(?s)<table\b[^>]*>.*?</table>`).FindString(page)
	rows := regexp.MustCompile(`(?s)<tr\b[^>]*>(.*?)</tr>`).FindAllStringSubmatch(table, -1)
	if len(rows) == 0 {
		return Price{}, fmt.Errorf("unknown pricing table")
	}
	cells := func(row string) []string {
		m := regexp.MustCompile(`(?s)<td\b[^>]*>(.*?)</td>`).FindAllStringSubmatch(row, -1)
		var out []string
		for _, v := range m {
			out = append(out, regexp.MustCompile(`<[^>]+>`).ReplaceAllString(v[1], ""))
		}
		return out
	}
	header := cells(rows[0][1])
	if len(header) != 3 || !strings.HasPrefix(header[1], "deepseek-flash") || header[2] != "deepseek-v4-pro" {
		return Price{}, fmt.Errorf("pricing model columns changed")
	}
	p := Price{Source: DeepSeekPricingURL, Expires: now.Add(time.Hour)}
	for i, row := range rows {
		c := cells(row[1])
		if len(c) != 4 || i+1 >= len(rows) {
			continue
		}
		kind := c[0]
		if kind != "1M INPUT TOKENS(CACHE MISS)" && kind != "1M OUTPUT TOKENS" {
			continue
		}
		peak := cells(rows[i+1][1])
		if len(peak) != 3 || peak[0] != "PEAK" || c[1] != "OFF-PEAK" {
			continue
		}
		a, e1 := strconv.ParseFloat(strings.TrimPrefix(c[2], "$"), 64)
		b, e2 := strconv.ParseFloat(strings.TrimPrefix(peak[1], "$"), 64)
		if e1 != nil || e2 != nil {
			continue
		}
		if strings.Contains(kind, "INPUT") {
			p.Input = max(a, b)
		} else {
			p.Output = max(a, b)
		}
	}
	if !p.Valid(now) {
		return Price{}, fmt.Errorf("current input/output prices unavailable")
	}
	return p, nil
}
