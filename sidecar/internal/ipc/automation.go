package ipc

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	engine "easy-command/sidecar/internal/exec"
	"easy-command/sidecar/internal/history"
	"easy-command/sidecar/internal/sched"
	"easy-command/sidecar/internal/script"
	"easy-command/sidecar/internal/store"
)

type automation struct {
	db        *store.Store
	scheduler *sched.Scheduler
}

type hotkeyConfig struct {
	Enabled     bool   `json:"enabled"`
	Accelerator string `json:"accelerator"`
}

func (a *automation) hotkey() (hotkeyConfig, error) {
	var config struct {
		Hotkey hotkeyConfig `json:"hotkey"`
	}
	err := a.db.ReadJSON(a.db.Path("config", "settings.json"), &config)
	return config.Hotkey, err
}

func (a *automation) route(req request, library *script.Library, ex execution) (any, func(engine.Emit), error) {
	invalid := &engine.Error{Code: "INVALID_PARAM", Message: "请求参数无效"}
	switch req.Action {
	case "ui.getState":
		if decode(req.Payload, &struct{}{}) != nil {
			return nil, nil, invalid
		}
		value, err := library.UIState()
		return value, nil, err
	case "ui.setSelected":
		var p struct {
			ScriptID string `json:"scriptId"`
		}
		if decode(req.Payload, &p) != nil {
			return nil, nil, invalid
		}
		value, err := library.SetSelected(p.ScriptID)
		return value, nil, err
	case "hotkey.trigger":
		if decode(req.Payload, &struct{}{}) != nil {
			return nil, nil, invalid
		}
		target, err := library.Target(false)
		if err != nil {
			return nil, nil, err
		}
		if target.Dangerous {
			return map[string]any{"status": "needs_confirm", "scriptId": target.ID, "name": target.Name}, nil, nil
		}
		return map[string]any{"status": "ready", "scriptId": target.ID, "name": target.Name, "dangerous": false}, nil, nil
	case "tray.runLast":
		if decode(req.Payload, &struct{}{}) != nil {
			return nil, nil, invalid
		}
		target, err := library.Target(true)
		if err != nil {
			return nil, nil, err
		}
		if target.Dangerous {
			return nil, nil, &trayConfirmation{target.ID, target.Name}
		}
		id, collect, err := ex.engine.StartTriggered(target.ID, nil, "tray", false)
		return map[string]string{"runId": id}, collect, err
	}
	if a == nil || a.scheduler == nil {
		return nil, nil, &engine.Error{Code: "IO_ERROR", Message: "调度器不可用"}
	}
	now := time.Now()
	switch req.Action {
	case "sched.list":
		if decode(req.Payload, &struct{}{}) != nil {
			return nil, nil, invalid
		}
		rules, paused := a.scheduler.List()
		return map[string]any{"rules": rules, "paused": paused}, nil, nil
	case "sched.upsert":
		var p struct {
			Rule *sched.Rule `json:"rule"`
		}
		if decode(req.Payload, &p) != nil || p.Rule == nil {
			return nil, nil, invalid
		}
		r := p.Rule
		if !overlaySize(r.Args) {
			return nil, nil, invalid
		}
		m, err := library.Metadata(r.ScriptID)
		if err != nil {
			return nil, nil, err
		}
		for key := range r.Args {
			allowed := false
			for _, param := range m.Params {
				if param.Key == key && param.Type != "password" {
					allowed = true
					break
				}
			}
			if !allowed {
				return nil, nil, invalid
			}
		}
		if r.ID == "" {
			r.ID, err = script.NewID()
			if err != nil {
				return nil, nil, err
			}
		} else if !validUUID(r.ID) {
			return nil, nil, invalid
		}
		return map[string]any{"rule": r}, nil, a.scheduler.Upsert(*r, now)
	case "sched.delete":
		var p struct {
			RuleID string `json:"ruleId"`
		}
		if decode(req.Payload, &p) != nil || !validUUID(p.RuleID) {
			return nil, nil, invalid
		}
		return struct{}{}, nil, a.scheduler.Delete(p.RuleID)
	case "sched.pauseAll", "sched.resumeAll":
		if decode(req.Payload, &struct{}{}) != nil {
			return nil, nil, invalid
		}
		paused := req.Action == "sched.pauseAll"
		return map[string]bool{"paused": paused}, nil, a.scheduler.Pause(paused, now)
	}
	return nil, nil, invalid
}

type trayConfirmation struct{ id, name string }

func (t *trayConfirmation) Error() string { return "危险脚本需要确认" }

func fire(rule sched.Rule, library *script.Library, ex execution, records *history.History, emit engine.Emit) {
	m, metaErr := library.Metadata(rule.ScriptID)
	id, collect, err := ex.engine.StartTriggered(rule.ScriptID, rule.Args, "schedule", false)
	if err == nil {
		emit("sched.fired", map[string]any{"ruleId": rule.ID, "scriptId": rule.ScriptID, "runId": id, "dangerous": m.Dangerous})
		collect(emit)
		return
	}
	if records == nil {
		return
	}
	code := executionFailure(err, nil).Code
	status := "failed"
	if code == "SCRIPT_ALREADY_RUNNING" {
		status = "skipped"
	}
	id, idErr := script.NewID()
	if idErr != nil {
		return
	}
	if metaErr != nil {
		m.Name = rule.ScriptID
	}
	now := time.Now()
	// Keep the failure reason machine-readable and never persist raw launch errors.
	record := history.Record{ID: id, ScriptID: rule.ScriptID, ScriptName: m.Name, StartedAt: now, EndedAt: &now,
		Status: status, Trigger: "schedule", ReasonCode: code, ArgsMasked: history.Snapshot(m.Params, rule.Args)}
	if err := records.Add(record); err != nil {
		emit("system.log", map[string]any{"code": "HISTORY_WRITE_FAILED", "runId": id, "message": "调度历史未保存"})
	}
}

func validateHotkey(config *hotkeyConfig) bool {
	if config == nil {
		return true
	}
	config.Accelerator = strings.TrimSpace(config.Accelerator)
	return len(config.Accelerator) <= 128
}

func scheduleFailure(err error) *protocolError {
	var e *sched.Error
	if errors.As(err, &e) {
		return &protocolError{e.Code, e.Message, struct{}{}}
	}
	var t *trayConfirmation
	if errors.As(err, &t) {
		return &protocolError{"NEEDS_DANGEROUS_CONFIRM", t.Error(), map[string]string{"scriptId": t.id, "name": t.name}}
	}
	return executionFailure(err, nil)
}

func overlaySize(args map[string]json.RawMessage) bool {
	data, err := json.Marshal(args)
	return err == nil && len(data) <= 128<<10
}
