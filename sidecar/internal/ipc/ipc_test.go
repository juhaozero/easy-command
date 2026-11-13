package ipc

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestServeRecoversAndShutsDown(t *testing.T) {
	const id = "8ae19b4b-84f9-4ae6-aa75-baf309b75110"
	req := func(action string) string {
		return `{"v":1,"type":"req","requestId":"` + id + `","action":"` + action + `","payload":{}}` + "\n"
	}
	input := "debug\n" + strings.Repeat("x", maxLineBytes+1) + "\n" +
		req("unknown") + req("system.ping") + req("system.shutdown") + req("system.ping")
	var stdout bytes.Buffer
	if err := Serve(strings.NewReader(input), &stdout, "0.1.0", `C:\data`); err != nil {
		t.Fatal(err)
	}
	want := []string{
		`{"v":1,"type":"evt","event":"system.hello","payload":{"dataDir":"C:\\data","version":"0.1.0"}}`,
		`{"v":1,"type":"evt","event":"system.protocol_error","payload":{"code":"PROTOCOL_ERROR","message":"invalid JSONL request envelope","truncated":false}}`,
		`{"v":1,"type":"evt","event":"system.protocol_error","payload":{"code":"PROTOCOL_ERROR","message":"request line exceeds 1 MiB …[truncated]","truncated":true}}`,
		`{"v":1,"type":"res","requestId":"` + id + `","ok":false,"error":{"code":"PROTOCOL_ERROR","message":"unsupported action","details":{}}}`,
		`{"v":1,"type":"res","requestId":"` + id + `","ok":true,"payload":{"pong":true}}`,
		`{"v":1,"type":"res","requestId":"` + id + `","ok":true,"payload":{}}`,
	}
	lines := strings.Split(stdout.String(), "\n")
	if len(lines) != len(want)+1 || lines[len(lines)-1] != "" {
		t.Fatalf("unexpected JSONL output: %q", stdout.String())
	}
	for i, line := range want {
		if !json.Valid([]byte(lines[i])) || lines[i] != line {
			t.Errorf("line %d = %q, want %q", i+1, lines[i], line)
		}
	}
}
