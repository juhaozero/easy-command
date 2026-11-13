package sched

import (
	"testing"
	"time"

	"easy-command/sidecar/internal/store"
)

func TestRulesPauseRestartAndStop(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 13, 9, 0, 0, 0, time.Local)
	s, err := Open(db, now)
	if err != nil {
		t.Fatal(err)
	}
	rules := []Rule{
		{ID: "cron", Enabled: true, Rule: Timing{Type: "cron", Expr: "1 9 * * *"}},
		{ID: "daily", Enabled: true, Rule: Timing{Type: "daily", At: "09:01"}},
		{ID: "interval", Enabled: true, Rule: Timing{Type: "interval", EverySeconds: 10}},
	}
	for _, r := range rules {
		if err := s.Upsert(r, now); err != nil {
			t.Fatal(err)
		}
	}
	for _, bad := range []Timing{{Type: "cron", Expr: "* * * * * *"}, {Type: "interval", EverySeconds: 9}, {Type: "daily", At: "9:00"}} {
		if err := s.Upsert(Rule{ID: "invalid", Rule: bad}, now); err == nil {
			t.Fatal("accepted invalid timing")
		}
	}
	fired := map[string]int{}
	tick := func(at time.Time) { s.Tick(at, func(r Rule) { fired[r.ID]++ }) }
	tick(now.Add(9 * time.Second))
	tick(now.Add(10 * time.Second))
	if len(fired) != 1 || fired["interval"] != 1 {
		t.Fatalf("interval: %v", fired)
	}
	tick(now.Add(time.Minute))
	if fired["daily"] != 1 || fired["cron"] != 1 || fired["interval"] != 2 {
		t.Fatalf("three rules: %v", fired)
	}
	if err := s.Pause(true, now); err != nil {
		t.Fatal(err)
	}
	s, err = Open(db, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, paused := s.List(); !paused {
		t.Fatal("pause not restored")
	}
	clear(fired)
	tick(now.Add(time.Minute))
	if len(fired) != 0 {
		t.Fatalf("paused fire: %v", fired)
	}
	if err := s.Pause(false, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	tick(now.Add(time.Minute + 10*time.Second))
	if fired["interval"] != 1 || len(fired) != 1 {
		t.Fatalf("resume: %v", fired)
	}
	s.Stop()
	tick(now.Add(24 * time.Hour))
	if fired["interval"] != 1 || len(fired) != 1 {
		t.Fatalf("stopping: %v", fired)
	}
}
