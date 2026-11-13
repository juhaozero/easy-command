package exec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"easy-command/sidecar/internal/bash"
	"easy-command/sidecar/internal/script"
	"easy-command/sidecar/internal/store"
)

func TestArguments(t *testing.T) {
	params := []script.Param{
		{Key: "first", Type: "string", CLI: script.CLI{Mode: "arg"}},
		{Key: "flag", Type: "boolean", CLI: script.CLI{Mode: "flag", Flag: "--on"}},
		{Key: "skip", Type: "boolean", CLI: script.CLI{Mode: "flag", Flag: "--skip"}},
		{Key: "variable", Type: "string", Default: json.RawMessage(`"hello world"`), CLI: script.CLI{Mode: "env", EnvName: "EC_TEST"}},
		{Key: "last", Type: "number", Default: json.RawMessage("0"), CLI: script.CLI{Mode: "arg"}},
	}
	args, env, err := arguments(params, map[string]json.RawMessage{"first": json.RawMessage(`"two words"`), "flag": json.RawMessage("true"), "skip": json.RawMessage("false")}, nil)
	if err != nil || !reflect.DeepEqual(args, []string{"two words", "--on", "0"}) || !reflect.DeepEqual(env, []string{"EC_TEST=hello world"}) {
		t.Fatalf("args=%q env=%q error=%v", args, env, err)
	}
}

func TestOutputEncodingAndBound(t *testing.T) {
	if value, bad := decodeLine([]byte{0xd6, 0xd0, 0xce, 0xc4}, 936); value != "中文" || bad {
		t.Fatalf("CP936: %q %v", value, bad)
	}
	if value, bad := decodeLine([]byte("中文"), 65001); value != "中文" || bad {
		t.Fatalf("UTF-8: %q %v", value, bad)
	}
	if value, bad := decodeLine([]byte{0xff}, 65001); value != "�" || !bad {
		t.Fatalf("replacement: %q %v", value, bad)
	}
	if value, bad := decodeLine([]byte{0x81}, 936); !strings.Contains(value, "�") || !bad {
		t.Fatalf("CP936 replacement: %q %v", value, bad)
	}
	input := append(bytes.Repeat([]byte{0}, maxOutputText+32), []byte("\r\nnext")...)
	var count int
	err := stream(bytes.NewReader(input), 65001, func(line string, truncated bool) {
		count++
		if count == 1 {
			encoded, _ := json.Marshal(line)
			if !truncated || !strings.HasSuffix(line, truncation) || len(encoded) > 1<<20-512 {
				t.Fatal("invalid truncation")
			}
		} else if line != "next" || truncated {
			t.Fatalf("next line: %q", line)
		}
	})
	if err != nil || count != 2 {
		t.Fatalf("stream: %v (%d lines)", err, count)
	}
}

func TestCompleteLogOutlivesProtocolTruncation(t *testing.T) {
	text := strings.Repeat("中文", maxOutputText/3)
	var logged strings.Builder
	var complete, truncated bool
	err := streamOutput(strings.NewReader(text+"\r\n"), 65001,
		func(_ string, clipped bool) { truncated = clipped },
		func(value string, continued bool) {
			logged.WriteString(value)
			complete = !continued
		})
	if err != nil || !complete || !truncated || logged.String() != text {
		t.Fatalf("output: bytes=%d complete=%v truncated=%v error=%v", logged.Len(), complete, truncated, err)
	}
}

type observed struct {
	event   string
	payload map[string]any
}

func startScript(t *testing.T, body string, params []script.Param, args map[string]json.RawMessage) (*Engine, string, chan observed) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "library with spaces"))
	if err != nil {
		t.Fatal(err)
	}
	library, err := script.Open(db)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "fixture.bat")
	if err := os.WriteFile(source, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := library.Import(source, "cancel")
	if err != nil {
		t.Fatal(err)
	}
	m.Params = params
	if _, _, err := library.UpdateMeta(m.ID, m.Fields); err != nil {
		t.Fatal(err)
	}
	engine := New(library, bash.New(db), io.Discard, nil)
	id, collect, err := engine.Start(m.ID, args)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = engine.Cancel(id) })
	events := make(chan observed, 16)
	collect(func(event string, payload map[string]any) { events <- observed{event, payload} })
	return engine, id, events
}

func nextEvent(t *testing.T, events <-chan observed, id string) observed {
	t.Helper()
	select {
	case event := <-events:
		if event.payload["runId"] != id {
			t.Fatalf("wrong run: %+v", event)
		}
		return event
	case <-time.After(10 * time.Second):
		t.Fatal("execution timed out")
	}
	return observed{}
}

func TestBatchArgumentsAndStreams(t *testing.T) {
	params := []script.Param{
		{Key: "arg", Label: "argument", Type: "string", CLI: script.CLI{Mode: "arg"}},
		{Key: "flag", Label: "flag", Type: "boolean", CLI: script.CLI{Mode: "flag", Flag: "--on"}},
		{Key: "env", Label: "environment", Type: "string", CLI: script.CLI{Mode: "env", EnvName: "EC_TEST"}},
	}
	_, id, events := startScript(t, "@echo off\r\necho [%1] [%2] [%EC_TEST%]\r\n>&2 echo on stderr\r\nexit /b 7\r\n", params,
		map[string]json.RawMessage{"arg": json.RawMessage(`"two & words"`), "flag": json.RawMessage("true"), "env": json.RawMessage(`"variable"`)})
	if event := nextEvent(t, events, id); event.event != "exec.started" {
		t.Fatalf("first event: %+v", event)
	}
	lines := map[string]string{}
	for {
		event := nextEvent(t, events, id)
		if event.event == "exec.exit" {
			if event.payload["status"] != "failed" || event.payload["exitCode"] != uint32(7) {
				t.Fatalf("exit: %+v", event)
			}
			break
		}
		lines[event.event] += fmt.Sprint(event.payload["line"])
	}
	if lines["exec.stdout"] != `["two & words"] ["--on"] [variable]` || lines["exec.stderr"] != "on stderr" {
		t.Fatalf("streams: %+v", lines)
	}
	if _, err := batchCommand("cmd.exe", "file.bat", []string{`a"b`}); err == nil {
		t.Fatal("embedded quote accepted")
	}
}

func TestCancelKillsBatchProcessTree(t *testing.T) {
	const body = "@echo off\r\npowershell.exe -NoProfile -Command \"$p = Start-Process -FilePath ping.exe -ArgumentList '-t','127.0.0.1' -PassThru; Write-Output $p.Id; Wait-Process -Id $p.Id\"\r\n"
	engine, id, events := startScript(t, body, nil, nil)
	var child syscall.Handle
	for child == 0 {
		event := nextEvent(t, events, id)
		if event.event == "exec.exit" || event.event == "exec.stderr" {
			t.Fatalf("unexpected event: %+v", event)
		}
		if event.event == "exec.stdout" {
			pid, err := strconv.ParseUint(strings.TrimSpace(event.payload["line"].(string)), 10, 32)
			if err != nil {
				t.Fatal(err)
			}
			child, err = syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
			if err != nil {
				t.Fatal(err)
			}
			defer syscall.CloseHandle(child)
		}
	}
	if err := engine.Cancel(id); err != nil {
		t.Fatal(err)
	}
	for {
		event := nextEvent(t, events, id)
		if event.event == "exec.exit" {
			if event.payload["status"] != "cancelled" || event.payload["exitCode"] != nil {
				t.Fatalf("exit: %+v", event)
			}
			break
		}
	}
	if state, err := syscall.WaitForSingleObject(child, 2000); err != nil || state != syscall.WAIT_OBJECT_0 {
		t.Fatalf("descendant still alive: %d %v", state, err)
	}
}
