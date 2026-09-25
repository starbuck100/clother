package budget

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jolehuit/clother/internal/usage"
)

func price() Price { return Price{Input: 1, Output: 1, Expires: time.Now().Add(time.Hour)} }
func TestParallelReservationsAndSettlement(t *testing.T) {
	l := Ledger{Dir: t.TempDir(), Session: "one", Config: Config{Daily: 1, Session: 0.6}}
	var n atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := l.Reserve(price(), 100000, 100000); e == nil {
				n.Add(1)
			}
		}()
	}
	wg.Wait()
	if n.Load() != 3 {
		t.Fatalf("reserved %d, want 3", n.Load())
	}
	l.Session = "two"
	done, e := l.Reserve(price(), 100000, 100000)
	if e != nil {
		t.Fatal(e)
	}
	input, output := 100, 100
	done(usage.Event{Outcome: "ok", Input: &input, Output: &output})
	s, e := l.Summary()
	if e != nil || s.Daily > 0.600201 || s.Session > 0.000201 {
		t.Fatalf("%+v %v", s, e)
	}
	// A crashed caller's outstanding reservations still count in another instance.
	l.Session = "three"
	if _, e = l.Reserve(price(), 200000, 200000); e == nil {
		t.Fatal("lost unresolved reservations")
	}
}
func TestBudgetFailClosed(t *testing.T) {
	for _, kind := range []string{"unknown-price", "expired", "zero", "corrupt", "locked"} {
		t.Run(kind, func(t *testing.T) {
			l := Ledger{Dir: t.TempDir(), Session: "test", Config: Defaults(nil)}
			p := price()
			switch kind {
			case "unknown-price":
				p = Price{}
			case "expired":
				p.Expires = time.Now().Add(-time.Second)
			case "zero":
				l.Config.Daily = 0
			case "corrupt":
				os.WriteFile(filepath.Join(l.Dir, "ledger.json"), []byte("broken"), 0600)
			case "locked":
				os.Mkdir(filepath.Join(l.Dir, "lock"), 0700)
			}
			if _, e := l.Reserve(p, 1, 1); e == nil {
				t.Fatal("allowed unsafe reservation")
			}
		})
	}
}
func TestBudgetProcessHelper(t *testing.T) {
	if os.Getenv("CLOTHER_BUDGET_TEST_CHILD") == "" {
		return
	}
	l := Ledger{Dir: os.Getenv("CLOTHER_BUDGET_TEST_DIR"), Session: ID(), Config: Defaults(nil)}
	if _, e := l.Reserve(price(), 250000, 250000); e != nil {
		os.Exit(3)
	}
	os.Exit(0)
}
func TestCrossProcessBudget(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	var n atomic.Int32
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestBudgetProcessHelper$")
			cmd.Env = append(os.Environ(), "CLOTHER_BUDGET_TEST_CHILD=1", "CLOTHER_BUDGET_TEST_DIR="+dir)
			if cmd.Run() == nil {
				n.Add(1)
			}
		}()
	}
	wg.Wait()
	if n.Load() != 2 {
		t.Fatalf("cross-process requests %d, want 2", n.Load())
	}
}
func TestDeepSeekPricingColumnsAndExpiry(t *testing.T) {
	page := `<table><tr><td>MODEL</td><td>deepseek-flash<sup>(1)</sup></td><td>deepseek-v4-pro</td></tr><tr><td>1M INPUT TOKENS<br>(CACHE MISS)</td><td>OFF-PEAK</td><td>$0.15</td><td>$0.66</td></tr><tr><td>PEAK</td><td>$0.30</td><td>$1.32</td></tr><tr><td>1M OUTPUT TOKENS</td><td>OFF-PEAK</td><td>$0.60</td><td>$1.98</td></tr><tr><td>PEAK</td><td>$1.20</td><td>$3.96</td></tr></table>`
	p, e := ParseDeepSeek(page, time.Now())
	if e != nil || p.Input != 0.3 || p.Output != 1.2 {
		t.Fatalf("%+v %v", p, e)
	}
	if _, e = ParseDeepSeek(`<table><tr><td>New prices</td></tr></table>`, time.Now()); e == nil {
		t.Fatal("accepted unknown layout")
	}
}
