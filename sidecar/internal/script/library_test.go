package script

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"easy-command/sidecar/internal/store"
)

func TestLibraryConflictEditAndDelete(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lib, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "sample.bat")
	write := func(content []byte) {
		t.Helper()
		if err := os.WriteFile(source, content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	mustCode := func(err error, code string) {
		t.Helper()
		var typed *Error
		if !errors.As(err, &typed) || typed.Code != code {
			t.Fatalf("error=%v, want %s", err, code)
		}
	}
	write([]byte("@echo first\r\n"))
	first, err := lib.Import(source, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	if first.Dangerous {
		t.Fatal("default dangerous must be false")
	}
	_, err = lib.Import(filepath.Join(lib.dir(first.ID), first.FileName), "overwrite")
	mustCode(err, "INVALID_PARAM")
	fields := first.Fields
	fields.Dangerous = true
	if _, _, err := lib.UpdateMeta(first.ID, fields); err != nil {
		t.Fatal(err)
	}
	write([]byte("@echo replacement\r\n"))
	_, err = lib.Import(source, "cancel")
	mustCode(err, "CONFLICT_CANCELLED")
	unchanged, data, _, _, err := lib.Get(first.ID, 0, "")
	if err != nil || !unchanged.Dangerous || string(data) != "@echo first\r\n" {
		t.Fatalf("cancel changed original: %+v %q %v", unchanged, data, err)
	}
	renamed, err := lib.Import(source, "rename")
	if err != nil || renamed.Name != "sample (2).bat" || renamed.ID == first.ID {
		t.Fatalf("rename=%+v %v", renamed, err)
	}
	overwrite, err := lib.Import(source, "overwrite")
	if err != nil || overwrite.ID != first.ID || overwrite.Dangerous {
		t.Fatalf("overwrite=%+v %v", overwrite, err)
	}
	original, err := os.ReadFile(source)
	if err != nil || string(original) != "@echo replacement\r\n" {
		t.Fatalf("source changed: %q %v", original, err)
	}
	fields = overwrite.Fields
	fields.Name = "edited.bat"
	fields.Dangerous = true
	if _, _, err := lib.UpdateMeta(first.ID, fields); err != nil {
		t.Fatal(err)
	}
	_, _, _, revision, err := lib.Get(first.ID, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("x"), MaxContent)
	var token string
	for offset := 0; offset < len(body); offset += ChunkSize {
		end := offset + ChunkSize
		var next int
		token, next, _, err = lib.UpdateContent(first.ID, body[offset:end], offset, revision, token, end == len(body))
		if err != nil || next != end {
			t.Fatalf("upload at %d: next=%d error=%v", offset, next, err)
		}
	}
	lib, err = Open(db)
	if err != nil {
		t.Fatal(err)
	}
	meta, got, next, revision, err := lib.Get(first.ID, 0, "")
	if err != nil || !meta.Dangerous || meta.Name != "edited.bat" {
		t.Fatalf("meta after reopen: %+v %v", meta, err)
	}
	for next != nil {
		var part []byte
		_, part, next, _, err = lib.Get(first.ID, *next, revision)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, part...)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("body length after reopen: %d", len(got))
	}
	_, _, _, err = lib.UpdateContent(first.ID, []byte("x"), MaxContent, revision, token, true)
	mustCode(err, "SIZE_LIMIT")
	indexPath := db.Path("library", "index.json")
	var summary []Summary
	if err := db.ReadJSON(indexPath, &summary); err != nil || len(summary) != 2 {
		t.Fatalf("index: %+v %v", summary, err)
	}
	state := db.Path("state", "ui_state.json")
	if err := db.WriteJSON(state, map[string]any{"lastSelectedScriptId": first.ID, "lastRunScriptId": first.ID, "keep": true}); err != nil {
		t.Fatal(err)
	}
	selected, last, err := lib.Delete(first.ID)
	if err != nil || !selected || !last {
		t.Fatalf("delete flags: %v %v %v", selected, last, err)
	}
	var saved map[string]any
	if err := db.ReadJSON(state, &saved); err != nil || saved["lastSelectedScriptId"] != nil || saved["lastRunScriptId"] != nil || saved["keep"] != true {
		t.Fatalf("state after delete: %+v %v", saved, err)
	}
	_, _, _, _, err = lib.Get(first.ID, 0, "")
	mustCode(err, "NOT_FOUND")
	if _, err := os.Stat(lib.dir(first.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("script directory still exists: %v", err)
	}
}

func TestOpenRecoversInterruptedReplaceAndDelete(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lib, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "recover.sh")
	if err := os.WriteFile(source, []byte("echo original"), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := lib.Import(source, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Move(lib.dir(m.ID), lib.dir(".replace-"+m.ID)); err != nil {
		t.Fatal(err)
	}
	lib, err = Open(db)
	if err != nil {
		t.Fatal(err)
	}
	_, body, _, _, err := lib.Get(m.ID, 0, "")
	if err != nil || string(body) != "echo original" {
		t.Fatalf("original not restored: %q %v", body, err)
	}
	state := db.Path("state", "ui_state.json")
	if err := db.WriteJSON(state, map[string]string{"lastSelectedScriptId": m.ID, "lastRunScriptId": m.ID}); err != nil {
		t.Fatal(err)
	}
	trash := lib.dir(".delete-" + m.ID)
	if err := store.Move(lib.dir(m.ID), trash); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(db); err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := db.ReadJSON(state, &saved); err != nil || saved["lastSelectedScriptId"] != nil || saved["lastRunScriptId"] != nil {
		t.Fatalf("state not recovered: %+v %v", saved, err)
	}
	if _, err := os.Stat(trash); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("trash still present: %v", err)
	}
}

func TestFieldsValidatePresetsRequireChoices(t *testing.T) {
	fields := Fields{
		Name:    "demo.bat",
		Enabled: true,
		Params: []Param{{
			Key: "action", Label: "动作", Type: "string", Required: true,
			CLI: CLI{Mode: "arg"},
			Choices: []Choice{{Label: "列出", Value: "list"}, {Label: "切换", Value: "use"}},
		}},
		Presets: []Preset{{
			ID: "p1", Name: "列出版本", Values: map[string]string{"action": "list"},
		}},
		DefaultPresetID: "p1",
	}
	if err := fields.validate(); err != nil {
		t.Fatal(err)
	}
	fields.Presets[0].Values["action"] = "other"
	if err := fields.validate(); err == nil {
		t.Fatal("expected invalid choice to fail")
	}
}

func TestImportDirLoadsBatAndSh(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lib, err := Open(db)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, body := range map[string]string{
		"one.bat":     "@echo one\r\n",
		"two.sh":      "echo two\n",
		"skip.txt":    "ignore",
		"nested.bat":  "@echo nested\r\n",
	} {
		if name == "nested.bat" {
			sub := filepath.Join(dir, "sub")
			if err := os.Mkdir(sub, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(sub, name), []byte(body), 0600); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	first, err := lib.Import(filepath.Join(dir, "one.bat"), "cancel")
	if err != nil {
		t.Fatal(err)
	}
	result, err := lib.ImportDir(dir, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Imported) != 1 || result.Imported[0].Name != "two.sh" {
		t.Fatalf("imported=%+v", result.Imported)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Code != "CONFLICT_CANCELLED" {
		t.Fatalf("skipped=%+v", result.Skipped)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("failed=%+v", result.Failed)
	}
	if first.ID == result.Imported[0].ID {
		t.Fatal("folder import reused existing id")
	}
}
