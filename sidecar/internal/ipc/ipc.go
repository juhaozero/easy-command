package ipc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"easy-command/sidecar/internal/script"
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
	Version int            `json:"v"`
	Type    string         `json:"type"`
	ID      string         `json:"requestId,omitempty"`
	OK      *bool          `json:"ok,omitempty"`
	Event   string         `json:"event,omitempty"`
	Payload any            `json:"payload,omitempty"`
	Error   *protocolError `json:"error,omitempty"`
}

// Serve sends the startup event and handles requests until shutdown or EOF.
func Serve(input io.Reader, output io.Writer, engineVersion, dataDir string) error {
	return serve(input, output, nil, engineVersion, dataDir, nil)
}

func ServeLibrary(input io.Reader, output, diagnostic io.Writer, engineVersion, dataDir string, library *script.Library) error {
	return serve(input, output, diagnostic, engineVersion, dataDir, library)
}

func serve(input io.Reader, output, diagnostic io.Writer, engineVersion, dataDir string, library *script.Library) error {
	reader := bufio.NewReader(input)
	if err := writeMessage(output, message{
		Type: "evt", Event: "system.hello",
		Payload: map[string]string{"version": engineVersion, "dataDir": dataDir},
	}); err != nil {
		return err
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
			response, shutdown = route(req, library, diagnostic)
		}

		if err := writeMessage(output, response); err != nil {
			return err
		}
		if shutdown {
			return nil
		}
	}
}

func route(req request, library *script.Library, diagnostic io.Writer) (message, bool) {
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
	case "script.list", "script.get", "script.import", "script.updateMeta", "script.updateContent", "script.delete":
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
