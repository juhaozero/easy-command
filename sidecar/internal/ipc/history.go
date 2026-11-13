package ipc

import (
	"errors"
	"io"

	"easy-command/sidecar/internal/history"
	"easy-command/sidecar/internal/logretain"
	"easy-command/sidecar/internal/script"
)

func routeHistory(req request, library *script.Library, records *history.History) (any, error) {
	switch req.Action {
	case "history.list":
		var filter history.Filter
		if decode(req.Payload, &filter) != nil || filter.ScriptID != "" && !validUUID(filter.ScriptID) {
			return nil, history.ErrInvalid
		}
		return records.List(filter)
	case "history.get":
		var p struct {
			RunID     string `json:"runId"`
			LogCursor *int64 `json:"logCursor"`
		}
		if decode(req.Payload, &p) != nil || !validUUID(p.RunID) || p.LogCursor != nil && *p.LogCursor < 0 {
			return nil, history.ErrInvalid
		}
		record, page, err := records.Get(p.RunID, p.LogCursor)
		if err != nil {
			return nil, err
		}
		exists, err := library.Exists(record.ScriptID)
		if err != nil {
			return nil, err
		}
		reply := map[string]any{"record": record, "scriptDeleted": !exists}
		if page != nil {
			reply["log"] = page
		}
		return reply, nil
	}
	return nil, history.ErrInvalid
}

type desktopSettings struct {
	CloseToTray         *bool `json:"closeToTray"`
	FirstCloseTipShown  *bool `json:"firstCloseTipShown"`
	OnboardingCompleted *bool `json:"onboardingCompleted"`
	Updater             *struct {
		CheckOnStartup bool `json:"checkOnStartup"`
	} `json:"updater"`
}

func (p desktopSettings) patch(fields map[string]any) {
	if p.CloseToTray != nil {
		fields["closeToTray"] = *p.CloseToTray
	}
	if p.FirstCloseTipShown != nil {
		fields["firstCloseTipShown"] = *p.FirstCloseTipShown
	}
	if p.OnboardingCompleted != nil {
		fields["onboardingCompleted"] = *p.OnboardingCompleted
	}
	if p.Updater != nil {
		fields["updater"] = p.Updater
	}
}

func routeSettings(req request, records *history.History, a *automation, requestCleanup func()) (any, bool, error) {
	switch req.Action {
	case "settings.get":
		if decode(req.Payload, &struct{}{}) != nil {
			return nil, false, history.ErrInvalid
		}
		policy, err := records.Policy()
		if err != nil {
			return nil, false, err
		}
		hotkey, err := a.hotkey()
		if err != nil {
			return nil, false, err
		}
		var config desktopSettings
		if err := a.db.ReadJSON(a.db.Path("config", "settings.json"), &config); err != nil {
			return nil, false, err
		}
		fields := map[string]any{"logRetention": policy, "hotkey": hotkey,
			"closeToTray": true, "firstCloseTipShown": false, "onboardingCompleted": false,
			"updater": map[string]bool{"checkOnStartup": false}}
		config.patch(fields)
		return fields, false, nil
	case "settings.set":
		var p struct {
			desktopSettings
			Policy *logretain.Policy `json:"logRetention"`
			Hotkey *hotkeyConfig     `json:"hotkey"`
		}
		if decode(req.Payload, &p) != nil ||
			(p.Policy != nil && !p.Policy.Valid()) || !validateHotkey(p.Hotkey) {
			return nil, false, history.ErrInvalid
		}
		patch := make(map[string]any)
		p.desktopSettings.patch(patch)
		if p.Policy != nil {
			patch["logRetention"] = p.Policy
		}
		if p.Hotkey != nil {
			patch["hotkey"] = p.Hotkey
		}
		if len(patch) == 0 {
			return nil, false, history.ErrInvalid
		}
		if err := a.db.UpdateJSONFields(a.db.Path("config", "settings.json"), patch); err != nil {
			return nil, false, err
		}
		if p.Policy != nil {
			requestCleanup()
		}
		return patch, p.Hotkey != nil, nil
	}
	return nil, false, history.ErrInvalid
}

func historyFailure(err error, diagnostic io.Writer) *protocolError {
	switch {
	case errors.Is(err, history.ErrInvalid), errors.Is(err, logretain.ErrCursor):
		return &protocolError{"INVALID_PARAM", "历史查询、日志游标或保留设置无效", struct{}{}}
	case errors.Is(err, history.ErrNotFound):
		return &protocolError{"NOT_FOUND", "执行记录不存在", struct{}{}}
	default:
		return scriptFailure(err, diagnostic)
	}
}
