package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

// Tone keeps status meaning visible in plain output as well as color terminals.
func (o *Output) Tone(tone, text string) string {
	text = clean(text)
	if !o.Color || o.Format != FormatHuman {
		return text
	}
	code := map[string]string{"good": "32", "warn": "33", "bad": "31", "muted": "90", "title": "1;36"}[tone]
	if code == "" {
		return text
	}
	return "\033[" + code + "m" + text + "\033[0m"
}
func clean(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
}
func (o *Output) Section(title string) {
	if o.Quiet {
		return
	}
	fmt.Fprintln(o.Stdout)
	fmt.Fprintln(o.Stdout, o.Tone("title", title))
	fmt.Fprintln(o.Stdout, o.Tone("muted", strings.Repeat("-", min(64, max(24, len(title))))))
}
func (o *Output) Field(label, value string) {
	o.detail("          "+fmt.Sprintf("%-15s", clean(label)), value)
}
func (o *Output) Check(tone, badge, label, value string) {
	prefix := "  " + o.Tone(tone, fmt.Sprintf("%-8s", "["+badge+"]")) + fmt.Sprintf("%-15s", clean(label))
	o.detail(prefix, value)
}
func (o *Output) detail(prefix, value string) {
	if o.Quiet {
		return
	}
	// Keep fields readable in a standard 80-column Windows console. ANSI bytes
	// are not display columns; continuation aligns with the field value.
	visible := 0
	escape := false
	for _, r := range prefix {
		if r == '\033' {
			escape = true
		}
		if !escape {
			visible++
		}
		if escape && r == 'm' {
			escape = false
		}
	}
	width := max(20, 78-visible)
	text := []rune(clean(value))
	first := true
	for len(text) > width {
		cut := width
		for i := width; i > width/2; i-- {
			if text[i] == ' ' || text[i] == '/' {
				cut = i
				break
			}
		}
		if !first {
			prefix = strings.Repeat(" ", visible)
		}
		fmt.Fprintln(o.Stdout, prefix+strings.TrimSpace(string(text[:cut])))
		text = text[cut:]
		first = false
	}
	if !first {
		prefix = strings.Repeat(" ", visible)
	}
	fmt.Fprintln(o.Stdout, prefix+strings.TrimSpace(string(text)))
}
func Number(n int) string {
	s := strconv.Itoa(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		if s[i-1] == '-' {
			break
		}
		s = s[:i] + "," + s[i:]
	}
	return s
}

// Meter only receives measured values and a known positive limit. Unknown and
// disabled limits have explicit text, never a misleading empty progress bar.
func (o *Output) Meter(used, limit float64) string {
	if math.IsNaN(used) || math.IsInf(used, 0) || used < 0 || math.IsNaN(limit) || math.IsInf(limit, 0) || limit <= 0 {
		return o.Tone("muted", "[------ unknown ------]")
	}
	ratio := used / limit
	filled := int(math.Min(1, ratio) * 20)
	if used > 0 && filled == 0 {
		filled = 1
	}
	tone := "good"
	if ratio >= 0.8 {
		tone = "warn"
	}
	if ratio >= 1 {
		tone = "bad"
	}
	return o.Tone(tone, "["+strings.Repeat("#", filled)+strings.Repeat(".", 20-filled)+"]") + fmt.Sprintf(" %3.0f%%", ratio*100)
}
func (o *Output) BudgetLine(label string, used, limit float64) {
	if o.Quiet {
		return
	}
	if limit == 0 {
		o.Field(label, "Paid requests disabled ($0.00 limit)")
		return
	}
	fmt.Fprintf(o.Stdout, "          %-15s%s  $%.3f / $%.2f\n", label, o.Meter(used, limit), used, limit)
}
