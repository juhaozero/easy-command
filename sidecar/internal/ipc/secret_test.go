package ipc

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"easy-command/sidecar/internal/bash"
	engine "easy-command/sidecar/internal/exec"
	"easy-command/sidecar/internal/script"
	"easy-command/sidecar/internal/store"
)

func TestSecretResponsesAndExecutionFailures(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	lib, err := script.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "fixture.bat")
	if err := os.WriteFile(source, []byte("@echo off\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	meta, err := lib.Import(source, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	meta.Params = []script.Param{{Key: "token", Type: "password", Label: "Token", CLI: script.CLI{Mode: "env", EnvName: "TOKEN"}}}
	if _, _, err := lib.UpdateMeta(meta.ID, meta.Fields); err != nil {
		t.Fatal(err)
	}
	settings := bash.New(db)
	ex := execution{engine: engine.New(lib, settings, io.Discard, nil), settings: settings}
	call := func(action string, fields map[string]string) message {
		t.Helper()
		fields["scriptId"] = meta.ID
		payload, err := json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		reply, _ := route(request{Version: 1, Type: "req", ID: "c8f2a115-079a-4887-b683-001a9cd092d6", Action: action, Payload: payload}, lib, ex, nil, func() {}, io.Discard)
		return reply
	}
	run := func(code string) {
		t.Helper()
		reply := call("script.run", map[string]string{"trigger": "manual"})
		if reply.Error == nil || reply.Error.Code != code || reply.collect != nil {
			t.Fatalf("expected %s without starting", code)
		}
	}
	run("SECRET_NOT_FOUND")
	const plaintext = "dpapi-fixture-private"
	saved := call("secret.set", map[string]string{"paramKey": "token", "plaintext": plaintext})
	if !*saved.OK {
		t.Fatalf("set failed: %+v", saved.Error)
	}
	path := db.Path("library", "scripts", meta.ID, "meta.json")
	var disk map[string]json.RawMessage
	if err := db.ReadJSON(path, &disk); err != nil {
		t.Fatal(err)
	}
	var entries []struct {
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.Unmarshal(disk["secrets"], &entries); err != nil || len(entries) != 1 || entries[0].Ciphertext == "" {
		t.Fatal("missing ciphertext")
	}
	raw, err := os.ReadFile(path)
	if err != nil || bytes.Contains(raw, []byte(plaintext)) {
		t.Fatal("plaintext leaked to metadata")
	}
	got := call("script.get", map[string]string{})
	var response bytes.Buffer
	if err := writeMessage(&response, got); err != nil {
		t.Fatal(err)
	}
	if !*got.OK || !bytes.Contains(response.Bytes(), []byte(`"passwordStored":true`)) || bytes.Contains(response.Bytes(), []byte(entries[0].Ciphertext)) || bytes.Contains(response.Bytes(), []byte(plaintext)) || bytes.Contains(response.Bytes(), []byte(`"secrets"`)) {
		t.Fatal("public response contains wrong password state")
	}
	disk["secrets"] = json.RawMessage(`[{"key":"token","ciphertext":"aW52YWxpZA==","updatedAt":"2026-11-13T12:00:00Z"}]`)
	if err := db.WriteJSON(path, disk); err != nil {
		t.Fatal(err)
	}
	run("SECRET_DECRYPT_FAILED")
	if reply := call("secret.clear", map[string]string{"paramKey": "token"}); !*reply.OK {
		t.Fatal("clear failed")
	}
	run("SECRET_NOT_FOUND")
}
