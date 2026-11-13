package script

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"easy-command/sidecar/internal/store"
)

func TestSelectedScriptValidationAndInvalidation(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	l, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	mustCode := func(err error, code string) {
		t.Helper()
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != code {
			t.Fatalf("want %s, got %v", code, err)
		}
	}
	_, err = l.Target(false)
	mustCode(err, "NO_SELECTION")
	id, err := NewID()
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.SetSelected(id)
	mustCode(err, "NOT_FOUND")
	file := filepath.Join(t.TempDir(), "select.bat")
	if err := os.WriteFile(file, []byte("@echo off"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := l.Import(file, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.SetSelected(m.ID); err != nil {
		t.Fatal(err)
	}
	if err := l.MarkRun(m.ID); err != nil {
		t.Fatal(err)
	}
	m.Enabled = false
	if _, cleared, err := l.UpdateMeta(m.ID, m.Fields); err != nil || !cleared {
		t.Fatalf("disable: %v %v", cleared, err)
	}
	_, err = l.SetSelected(m.ID)
	mustCode(err, "DISABLED")
	_, err = l.Target(false)
	mustCode(err, "NO_SELECTION")
	if _, recent, err := l.Delete(m.ID); err != nil || !recent {
		t.Fatalf("delete: %v %v", recent, err)
	}
}
