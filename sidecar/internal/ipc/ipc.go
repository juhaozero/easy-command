package ipc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
	"unicode/utf8"

	"easy-command/sidecar/internal/bash"
	engine "easy-command/sidecar/internal/exec"
	"easy-command/sidecar/internal/history"
	"easy-command/sidecar/internal/sched"
	"easy-command/sidecar/internal/script"
	"easy-command/sidecar/internal/store"
)

const (
	version      = 1
	maxLineBytes = 1 << 20
	truncated    = "…[truncated]"
)

type request struct {
	Version int             `json:"v"`
	Type    string          `json:"type"`
	ID      string          `json:"requestId"`
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload"`
}

type protocolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details"`
}

type message struct {
	Version         int            `json:"v"`
	Type            string         `json:"type"`
	ID              string         `json:"requestId,omitempty"`
	OK              *bool          `json:"ok,omitempty"`
	Event           string         `json:"event,omitempty"`
	Payload         any            `json:"payload,omitempty"`
	Error           *protocolError `json:"error,omitempty"`
	collect         func(engine.Emit)
	settingsChanged string
}

// Serve sends the startup event and handles requests until shutdown or EOF.
func Serve(input io.Reader, output io.Writer, engineVersion, dataDir string) error {
	return serve(input, output, nil, engineVersion, dataDir, nil, execution{}, nil)
}

func ServeLibrary(input io.Reader, output, diagnostic io.Writer, engineVersion, dataDir string, library *script.Library, settings bash.Settings, records *history.History, db *store.Store) error {
	scheduler, err := sched.Open(db, time.Now())
	if err != nil && diagnostic != nil {
		_, _ = fmt.Fprintf(diagnostic, "schedule startup: %v\n", err)
	}
	return serve(input, output, diagnostic, engineVersion, dataDir, library,
		execution{engine.New(library, settings, diagnostic, records), settings, &automation{db, scheduler}}, records)
}

func serve(input io.Reader, output, diagnostic io.Writer, engineVersion, dataDir string, library *script.Library, exec execution, records *history.History) error {
	reader := bufio.NewReader(input)
	var outputMu sync.Mutex
	write := func(msg message) error {
		outputMu.Lock()
		defer outputMu.Unlock()
		return writeMessage(output, msg)
	}
	if err := writeMessage(output, message{
		Type: "evt", Event: "system.hello",
		Payload: map[string]string{"version": engineVersion, "dataDir": dataDir},
	}); err != nil {
		return err
	}
	cleanup := make(chan struct{}, 1)
	requestCleanup := func() {
		select {
		case cleanup <- struct{}{}:
		default:
		}
	}
	if records != nil {
		records.Recover(time.Now())
		done := make(chan struct{})
		defer close(done)
		go func() {
			ticker := time.NewTicker(6 * time.Hour)
			defer ticker.Stop()
			records.Clean(time.Now())
			for {
				select {
				case <-done:
					return
				case <-ticker.C:
				case <-cleanup:
				}
				records.Clean(time.Now())
			}
		}()
	}
	if exec.automation != nil && exec.automation.scheduler != nil {
		scheduler := exec.automation.scheduler
		done := make(chan struct{})
		defer close(done)
		defer scheduler.Stop()
		go func() {
			ticker := time.NewTicker(time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-done:
					return
				case now := <-ticker.C:
					scheduler.Tick(now, func(rule sched.Rule) {
						fire(rule, library, exec, records, func(event string, payload map[string]any) {
							_ = write(message{Type: "evt", Event: event, Payload: payload})
						})
					})
				}
			}
		}()
	}

	for {
		line, overflow, err := readLine(reader)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return err
		}

		var req request
		var response message
		var shutdown bool
		switch {
		case overflow:
			response = invalidLine("request line exceeds 1 MiB "+truncated, true)
		case err != nil:
			response = invalidLine("request line is missing its newline", false)
		case !utf8.Valid(line) || json.Unmarshal(line, &req) != nil || !validUUID(req.ID):
			response = invalidLine("invalid JSONL request envelope", false)
		default:
			response, shutdown = route(req, library, exec, records, requestCleanup, diagnostic)
			if req.Action == "secret.set" {
				clear(req.Payload)
				clear(line)
			}
		}

		if shutdown && exec.automation != nil && exec.automation.scheduler != nil {
			exec.automation.scheduler.Stop()
		}
		if err := write(response); err != nil {
			return err
		}
		if response.settingsChanged != "" {
			_ = write(message{Type: "evt", Event: "system.log", Payload: map[string]any{"code": "SETTINGS_CHANGED", "scope": response.settingsChanged}})
		}
		if response.collect != nil {
			requestID := req.ID
			response.collect(func(event string, payload map[string]any) {
				_ = write(message{Type: "evt", ID: requestID, Event: event, Payload: payload})
			})
		}
		if shutdown {
			return nil
		}
	}
}

func route(req request, library *script.Library, exec execution, records *history.History, requestCleanup func(), diagnostic io.Writer) (message, bool) {
	ok := false
	response := message{Type: "res", ID: req.ID, OK: &ok}
	var payload map[string]json.RawMessage
	if req.Version != version || req.Type != "req" || req.Action == "" ||
		json.Unmarshal(req.Payload, &payload) != nil || payload == nil {
		response.Error = &protocolError{"PROTOCOL_ERROR", "expected a v1 request with an object payload", struct{}{}}
		return response, false
	}
	if (req.Action == "system.ping" || req.Action == "system.shutdown") && len(payload) != 0 {
		response.Error = &protocolError{"PROTOCOL_ERROR", "expected an empty payload object", struct{}{}}
		return response, false
	}

	switch req.Action {
	case "system.ping":
		ok = true
		response.Payload = map[string]bool{"pong": true}
	case "system.shutdown":
		ok = true
		response.Payload = struct{}{}
		return response, true
	case "script.list", "script.get", "script.import", "script.importDir", "script.updateMeta", "script.updateContent", "script.delete":
		if library == nil {
			response.Error = &protocolError{"IO_ERROR", "script library unavailable", struct{}{}}
			break
		}
		value, err := routeScript(library, req)
		if err != nil {
			response.Error = scriptFailure(err, diagnostic)
		} else {
			ok = true
			response.Payload = value
		}
	case "script.run", "script.cancel", "bash.detect", "bash.setPath":
		if exec.engine == nil {
			response.Error = &protocolError{"IO_ERROR", "execution engine unavailable", struct{}{}}
			break
		}
		value, collect, err := exec.route(req, library)
		if err != nil {
			response.Error = executionFailure(err, diagnostic)
		} else {
			ok = true
			response.Payload, response.collect = value, collect
		}
	case "secret.set", "secret.clear":
		if library == nil {
			response.Error = &protocolError{"IO_ERROR", "script library unavailable", struct{}{}}
			break
		}
		value, err := routeSecret(library, req)
		if err != nil {
			response.Error = scriptFailure(err, nil)
		} else {
			ok = true
			response.Payload = value
		}
	case "sched.list", "sched.upsert", "sched.delete", "sched.pauseAll", "sched.resumeAll", "ui.getState", "ui.setSelected", "hotkey.trigger", "tray.runLast":
		if library == nil || exec.engine == nil {
			response.Error = &protocolError{"IO_ERROR", "automation unavailable", struct{}{}}
			break
		}
		value, collect, err := exec.automation.route(req, library, exec)
		if err != nil {
			response.Error = scheduleFailure(err)
		} else {
			ok = true
			response.Payload, response.collect = value, collect
			if req.Action == "sched.pauseAll" || req.Action == "sched.resumeAll" {
				response.settingsChanged = "schedule"
			}
		}
	case "settings.get", "settings.set":
		if exec.automation == nil || records == nil {
			response.Error = &protocolError{"IO_ERROR", "settings unavailable", struct{}{}}
			break
		}
		value, changed, err := routeSettings(req, records, exec.automation, requestCleanup)
		if err != nil {
			response.Error = historyFailure(err, diagnostic)
		} else {
			ok = true
			response.Payload = value
			if changed {
				response.settingsChanged = "hotkey"
			}
		}
	case "history.list", "history.get":
		if records == nil {
			response.Error = &protocolError{"IO_ERROR", "history unavailable", struct{}{}}
			break
		}
		value, err := routeHistory(req, library, records)
		if err != nil {
			response.Error = historyFailure(err, diagnostic)
		} else {
			ok = true
			response.Payload = value
		}
	default:
		response.Error = &protocolError{"PROTOCOL_ERROR", "unsupported action", struct{}{}}
	}
	return response, false
}

func validUUID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, c := range id {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !('0' <= c && c <= '9' || 'a' <= c && c <= 'f' || 'A' <= c && c <= 'F') {
				return false
			}
		}
	}
	return true
}

func invalidLine(text string, overflow bool) message {
	return message{
		Type: "evt", Event: "system.protocol_error",
		Payload: map[string]any{"code": "PROTOCOL_ERROR", "message": text, "truncated": overflow},
	}
}

func writeMessage(output io.Writer, msg message) error {
	msg.Version = version
	line, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(line) > maxLineBytes {
		if msg.Type != "res" {
			return fmt.Errorf("output line exceeds 1 MiB %s", truncated)
		}
		ok := false
		return writeMessage(output, message{Type: "res", ID: msg.ID, OK: &ok,
			Error: &protocolError{"SIZE_LIMIT", "response exceeds 1 MiB " + truncated, map[string]bool{"truncated": true}},
		})
	}
	_, err = output.Write(append(line, '\n'))
	return err
}

// ReadSlice exposes bounded fragments, so even a line without LF cannot grow the buffer past the limit.
func readLine(reader *bufio.Reader) ([]byte, bool, error) {
	var line []byte
	overflow := false
	for {
		part, err := reader.ReadSlice('\n')
		part = bytes.TrimSuffix(part, []byte{'\n'})
		if !overflow {
			if len(part) > maxLineBytes-len(line) {
				line, overflow = nil, true
			} else {
				line = append(line, part...)
			}
		}
		switch err {
		case bufio.ErrBufferFull:
			continue
		case io.EOF:
			if overflow || len(line) != 0 {
				return line, overflow, io.ErrUnexpectedEOF
			}
		}
		return line, overflow, err
	}
}
