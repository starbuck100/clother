package commands

import (
	"fmt"
	"strings"

	"github.com/jolehuit/clother/internal/budget"
	"github.com/jolehuit/clother/internal/ui"
)

func printDoctor(out *ui.Output, report doctorReport, b budget.Summary, activeSession bool) {
	if out.Quiet {
		return
	}
	out.Section("CLOTHER DOCTOR")
	out.Line("  System checks and provider readiness")
	out.Section("SYSTEM")
	check := func(ok bool, label, yes, no string) {
		if ok {
			out.Check("good", "OK", label, yes)
		} else {
			out.Check("bad", "FIX", label, no)
		}
	}
	check(report.Native, "Claude Code", "Found", "Not found - install Claude Code")
	check(report.Launcher, "Clother", "Launcher found", "Missing - run clother install")
	check(report.BudgetReadable, "Budget ledger", "Readable", "Cannot read budget data")
	if report.BudgetLock {
		out.Check("warn", "BUSY", "Budget lock", "Present; another process may be writing")
	} else {
		out.Check("good", "OK", "Budget lock", "Unlocked")
	}

	var steps []string
	verified, untested, needsSetup := 0, 0, 0
	for _, p := range report.Providers {
		name := "OpenRouter"
		if p.Provider == "kilo" {
			name = "Kilo"
		}
		out.Section(strings.ToUpper(name))
		out.Field("Model", p.Model)
		if p.Catalog && p.Context > 0 {
			if p.Free {
				out.Check("good", "FREE", "Pricing", "Free model (catalog)")
			} else {
				out.Check("warn", "PAID", "Pricing", "Paid model selected")
			}
			out.Field("Context limit", ui.Number(p.Context)+" tokens")
			output := "Not reported"
			if p.Output > 0 {
				output = ui.Number(p.Output) + " tokens"
			}
			out.Field("Output limit", output)
		} else {
			out.Check("warn", "INFO", "Model limits", "No compatible live model metadata")
		}
		check(p.Catalog, "Model catalog", "Live lookup succeeded", "Unavailable")
		switch p.Auth {
		case "key missing":
			out.Check("warn", "SETUP", "API key", "Missing - OpenRouter cannot run")
		case "passed":
			out.Check("good", "OK", "API key", "Configured; authentication passed")
		case "failed":
			out.Check("bad", "FAIL", "API key", "Authentication check failed")
		case "free quota exhausted":
			out.Check("warn", "LIMIT", "Free quota", "Provider reports no free requests remaining")
		default:
			if p.Key {
				out.Check("muted", "INFO", "API key", "Configured; authentication not tested")
			} else {
				out.Check("muted", "INFO", "API key", "Optional - anonymous access enabled")
			}
		}
		if q := p.quota; q != nil {
			// Only provider-reported values are used for the quota meter.
			out.Line("          %-15s%s", "Free quota used", out.Meter(float64(q.Used), float64(q.Limit)))
			out.Field("Remaining", fmt.Sprintf("%s of %s requests (provider report)", ui.Number(q.Remaining), ui.Number(q.Limit)))
		} else if p.Provider == "kilo" {
			out.Field("Free quota", "Unknown - shared IP allowance")
		} else {
			out.Field("Free quota", "Unknown - no provider counter available")
		}
		switch {
		case strings.HasPrefix(p.Probe, "passed"):
			out.Check("good", "PASS", "Tool test", "Call + result + final OK verified")
			verified++
		case p.Probe == "not run":
			out.Check("muted", "--", "Tool test", "Not run; inference not verified")
		case strings.HasPrefix(p.Probe, "blocked"):
			out.Check("warn", "WAIT", "Tool test", p.Probe)
		default:
			out.Check("bad", "FAIL", "Tool test", p.Probe)
		}
		ready := p.Catalog && p.Context > 0 && (p.Provider == "kilo" || p.Auth == "passed")
		if !ready {
			needsSetup++
			if p.Auth == "key missing" || p.Auth == "failed" {
				steps = append(steps, name+": clother config "+p.Provider)
			} else if p.Auth == "free quota exhausted" {
				steps = append(steps, name+": clother usage "+p.Provider)
			} else {
				steps = append(steps, name+": check the configured model / catalog endpoint")
			}
		}
		if ready && !strings.HasPrefix(p.Probe, "passed") {
			untested++
			if p.Free {
				steps = append(steps, "Test "+name+": clother doctor "+p.Provider+" probe")
			} else {
				steps = append(steps, "Choose a free model: clother config "+p.Provider)
			}
		}
	}
	out.Section("PAID FALLBACK BUDGET")
	if report.BudgetReadable {
		out.BudgetLine("UTC day used", b.Daily, b.DailyLimit)
		if activeSession {
			out.BudgetLine("Session used", b.Session, b.SessionLimit)
		} else {
			out.Field("Session limit", fmt.Sprintf("$%.2f per launched session", b.SessionLimit))
		}
		out.Field("Accounting", "Local estimates + reservations; not provider billing")
	} else {
		out.Check("bad", "FIX", "Budget", "Unavailable; paid fallback is blocked")
	}
	out.Field("Change limits", "clother config budget <daily-USD> <session-USD>")
	out.Section("RESULT")
	out.Line("  %s  %s  %s", out.Tone("good", fmt.Sprintf("%d tool-tested", verified)), out.Tone("muted", fmt.Sprintf("%d unverified", untested)), out.Tone("warn", fmt.Sprintf("%d need setup", needsSetup)))
	if len(steps) > 0 {
		out.Line("")
		for i, step := range steps {
			out.Line("  %d. %s", i+1, step)
		}
	}
	out.Line("")
	out.Line("  Optional 'probe' sends two small free requests; quota may be used.")
	out.Line("  Export diagnostics: clother doctor --json")
	out.Line("")
}
