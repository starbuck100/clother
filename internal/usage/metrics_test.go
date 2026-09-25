package usage

import (
	"testing"
	"time"
)

func TestLocalMetricsScopeCooldownAndRecovery(t *testing.T) {
	now := time.Now()
	var events []Event
	for i := 0; i < 3; i++ {
		events = append(events, Event{At: now.Add(time.Duration(i-3) * time.Second), Provider: "kilo", Scope: "account", Model: "model", Outcome: "http_error", Status: 500})
	}
	events = append(events, Event{At: now, Provider: "kilo", Scope: "another", Model: "model", Outcome: "ok"})
	m := Measure(events, "kilo", "account", "model", now)
	if !m.Cooling(now) || m.Samples != 3 || m.Score() >= 0 {
		t.Fatalf("%+v", m)
	}
	if m.Cooling(now.Add(11 * time.Minute)) {
		t.Fatal("cooldown never expires")
	}
	events = append(events, Event{At: now, Provider: "kilo", Scope: "account", Model: "model", Outcome: "ok", ToolCalls: 1, DurationMS: 100})
	m = Measure(events, "kilo", "account", "model", now)
	if m.Cooling(now) || m.ToolResponses != 1 || m.MeanMS != 100 {
		t.Fatalf("%+v", m)
	}
	events = append(events, Event{At: now, Provider: "kilo", Scope: "account", Model: "model", Status: 429, Limit: &Limit{Kind: "free_hour", Scope: "provider"}})
	if m2 := Measure(events, "kilo", "account", "model", now); m2.Samples != 4 {
		t.Fatal("quota polluted reliability score")
	}
}
