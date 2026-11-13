package ipc

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"easy-command/sidecar/internal/script"
)

func decode(raw json.RawMessage, dst any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(dst)
}

func routeScript(l *script.Library, req request) (any, error) {
	invalid := &script.Error{Code: "INVALID_PARAM", Message: "请求字段缺失或无效"}
	switch req.Action {
	case "script.list":
		var p struct {
			AfterID string `json:"afterId"`
			Limit   int    `json:"limit"`
		}
		if decode(req.Payload, &p) != nil {
			return nil, invalid
		}
		items, next, err := l.List(p.AfterID, p.Limit)
		return map[string]any{"scripts": items, "nextAfterId": next}, err
	case "script.get":
		var p struct {
			ScriptID string `json:"scriptId"`
			Offset   int    `json:"offset"`
			Revision string `json:"revision"`
		}
		if decode(req.Payload, &p) != nil {
			return nil, invalid
		}
		meta, data, next, revision, err := l.Get(p.ScriptID, p.Offset, p.Revision)
		return map[string]any{"meta": meta, "contentBase64": data, "nextOffset": next, "revision": revision}, err
	case "script.import":
		var p struct {
			SourcePath     string `json:"sourcePath"`
			ConflictPolicy string `json:"conflictPolicy"`
		}
		if decode(req.Payload, &p) != nil {
			return nil, invalid
		}
		m, err := l.Import(p.SourcePath, p.ConflictPolicy)
		return map[string]any{"meta": m}, err
	case "script.updateMeta":
		var p struct {
			ScriptID string         `json:"scriptId"`
			Meta     *script.Fields `json:"meta"`
		}
		if decode(req.Payload, &p) != nil || p.Meta == nil {
			return nil, invalid
		}
		m, selected, err := l.UpdateMeta(p.ScriptID, *p.Meta)
		return map[string]any{"meta": m, "selectionInvalidated": selected}, err
	case "script.updateContent":
		var p struct {
			ScriptID string  `json:"scriptId"`
			Content  *[]byte `json:"contentBase64"`
			Offset   *int    `json:"offset"`
			Revision string  `json:"revision"`
			UploadID string  `json:"uploadId"`
			Final    *bool   `json:"final"`
		}
		if decode(req.Payload, &p) != nil || p.Content == nil || p.Offset == nil || p.Final == nil {
			return nil, invalid
		}
		token, next, m, err := l.UpdateContent(p.ScriptID, *p.Content, *p.Offset, p.Revision, p.UploadID, *p.Final)
		reply := map[string]any{"uploadId": token, "nextOffset": next}
		if m != nil {
			reply["meta"] = m
		}
		return reply, err
	case "script.delete":
		var p struct {
			ScriptID string `json:"scriptId"`
		}
		if decode(req.Payload, &p) != nil {
			return nil, invalid
		}
		selected, last, err := l.Delete(p.ScriptID)
		return map[string]any{"selectionInvalidated": selected, "lastRunInvalidated": last}, err
	}
	return nil, invalid
}

func scriptFailure(err error, diagnostic io.Writer) *protocolError {
	var known *script.Error
	if errors.As(err, &known) {
		return &protocolError{known.Code, known.Message, struct{}{}}
	}
	if diagnostic != nil {
		_, _ = io.WriteString(diagnostic, "script library: "+err.Error()+"\n")
	}
	return &protocolError{"IO_ERROR", "无法读写脚本库", struct{}{}}
}
