package history

import (
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"easy-command/sidecar/internal/logretain"
	"easy-command/sidecar/internal/script"
	"easy-command/sidecar/internal/store"
)

func TestLatestInterruptedAndRetention(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	earlier := now.AddDate(0, 0, -2)
	h := New(db, io.Discard)
	record := func(id string, at time.Time) Record {
		return Record{ID: id, ScriptID: "fixture", ScriptName: "fixture.bat", StartedAt: at, Status: "running", Trigger: "manual", LogRef: "exec-" + at.Format(logretain.DateLayout) + ".log"}
	}
	stale := record("old", earlier)
	live := record("live", now)
	for _, r := range []Record{stale, live} {
		if err := h.Begin(r); err != nil {
			t.Fatal(err)
		}
	}
	end := earlier.Add(time.Minute)
	stale.EndedAt, stale.Status = &end, "failed"
	if err := h.Finish(stale); err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(h.path(), os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(`{"broken":`); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	h = New(db, io.Discard)
	h.Recover(now.Add(time.Minute))
	page, err := h.List(Filter{Limit: 1})
	if err != nil || len(page.Runs) != 1 || page.Runs[0].Status != "interrupted" || page.NextCursor == nil {
		t.Fatalf("first page: %+v %v", page, err)
	}
	page, err = h.List(Filter{Cursor: *page.NextCursor, Status: "failed"})
	if err != nil || len(page.Runs) != 1 || page.Runs[0].ID != "old" {
		t.Fatalf("second page: %+v %v", page, err)
	}
	cursor := int64(0)
	r, log, err := h.Get("live", &cursor)
	if err != nil || r.EndedAt == nil || r.ExitCode != nil || len(log.Entries) != 2 || !strings.Contains(string(log.Entries[1]), "interrupted") {
		t.Fatalf("recovery: %+v %+v %v", r, log, err)
	}
	if err := h.clean(now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(db.Path("logs", stale.LogRef)); err != nil {
		t.Fatal(err)
	}
	if err := h.SetPolicy(logretain.Policy{Mode: "days", Days: 1}); err != nil {
		t.Fatal(err)
	}
	if err := h.clean(now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(db.Path("logs", stale.LogRef)); !os.IsNotExist(err) {
		t.Fatalf("old log: %v", err)
	}
	h = New(db, io.Discard)
	h.Recover(now)
	page, err = h.List(Filter{})
	if err != nil || len(page.Runs) != 1 || page.Runs[0].ID != "live" {
		t.Fatalf("persisted retention: %+v %v", page, err)
	}
}

func TestSnapshotMasksPasswordAndSensitiveValues(t *testing.T) {
	params := []script.Param{
		{Key: "password", Type: "password"}, {Key: "token", MaskInLog: true},
		{Key: "plain", Default: json.RawMessage(`"default"`)},
	}
	result := Snapshot(params, map[string]json.RawMessage{"password": json.RawMessage(`"secret"`), "token": json.RawMessage("42")})
	expected := map[string]json.RawMessage{"password": json.RawMessage(`"***"`), "token": json.RawMessage(`"***"`), "plain": json.RawMessage(`"default"`)}
	if !reflect.DeepEqual(result, expected) {
		t.Fatalf("snapshot: %s", result)
	}
}
