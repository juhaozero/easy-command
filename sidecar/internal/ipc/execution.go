package ipc

import (
	"encoding/json"
	"errors"
	"io"

	"easy-command/sidecar/internal/bash"
	engine "easy-command/sidecar/internal/exec"
	"easy-command/sidecar/internal/script"
)

type execution struct {
	engine     *engine.Engine
	settings   bash.Settings
	automation *automation
}

func (e execution) route(req request, library *script.Library) (any, func(engine.Emit), error) {
	invalid := &engine.Error{Code: "INVALID_PARAM", Message: "请求字段缺失或无效"}
	switch req.Action {
	case "script.run":
		var p struct {
			ScriptID  string                     `json:"scriptId"`
			Args      map[string]json.RawMessage `json:"args"`
			Trigger   string                     `json:"trigger"`
			Confirmed bool                       `json:"confirmed"`
		}
		if decode(req.Payload, &p) != nil || (p.Trigger != "manual" && p.Trigger != "hotkey" && p.Trigger != "tray") {
			return nil, nil, invalid
		}
		if p.Trigger == "hotkey" || p.Trigger == "tray" {
			target, err := library.Target(p.Trigger == "tray")
			if err != nil {
				return nil, nil, err
			}
			if target.ID != p.ScriptID {
				if p.Trigger == "tray" {
					return nil, nil, &engine.Error{Code: "NO_RECENT_RUN", Message: "最近运行的脚本已变化，请重新打开托盘菜单"}
				}
				return nil, nil, &engine.Error{Code: "SELECTION_DISABLED", Message: "选中脚本已变化，请重新触发快捷键"}
			}
		}
		id, collect, err := e.engine.StartTriggered(p.ScriptID, p.Args, p.Trigger, p.Confirmed)
		return map[string]string{"runId": id}, collect, err
	case "script.cancel":
		var p struct {
			RunID string `json:"runId"`
		}
		if decode(req.Payload, &p) != nil || !validUUID(p.RunID) {
			return nil, nil, invalid
		}
		return struct{}{}, nil, e.engine.Cancel(p.RunID)
	case "bash.detect":
		if decode(req.Payload, &struct{}{}) != nil {
			return nil, nil, invalid
		}
		result, err := e.settings.Detect()
		return result, nil, err
	case "bash.setPath":
		var p struct {
			Path json.RawMessage `json:"path"`
		}
		var path *string
		if decode(req.Payload, &p) != nil || json.Unmarshal(p.Path, &path) != nil {
			return nil, nil, invalid
		}
		result, err := e.settings.SetPath(path)
		if errors.Is(err, bash.ErrInvalidPath) {
			err = &engine.Error{Code: "INVALID_PARAM", Message: err.Error()}
		}
		return result, nil, err
	}
	return nil, nil, invalid
}

func executionFailure(err error, diagnostic io.Writer) *protocolError {
	var failure *engine.Error
	if errors.As(err, &failure) {
		return &protocolError{failure.Code, failure.Message, struct{}{}}
	}
	return scriptFailure(err, diagnostic)
}
