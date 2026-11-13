package script

import (
	"errors"

	"easy-command/sidecar/internal/state"
)

func (l *Library) UIState() (state.State, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return state.Get(l.db)
}

func (l *Library) SetSelected(id string) (state.State, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	m, err := l.meta(id)
	if err != nil {
		return state.State{}, err
	}
	if !m.Enabled {
		return state.State{}, fail("DISABLED", "脚本已禁用")
	}
	if err := state.Set(l.db, "lastSelectedScriptId", &id); err != nil {
		return state.State{}, err
	}
	return state.Get(l.db)
}

func (l *Library) MarkRun(id string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.meta(id); err != nil {
		var failure *Error
		if errors.As(err, &failure) && failure.Code == "NOT_FOUND" {
			return nil
		}
		return err
	}
	return state.Set(l.db, "lastRunScriptId", &id)
}

func (l *Library) Target(recent bool) (Meta, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	current, err := state.Get(l.db)
	if err != nil {
		return Meta{}, err
	}
	id, emptyCode, emptyText := current.Selected, "NO_SELECTION", "请先选择一个可用脚本"
	if recent {
		id, emptyCode, emptyText = current.Recent, "NO_RECENT_RUN", "没有最近运行的脚本"
	}
	if id == nil {
		return Meta{}, fail(emptyCode, emptyText)
	}
	m, err := l.meta(*id)
	var failure *Error
	if err != nil && !(errors.As(err, &failure) && (failure.Code == "NOT_FOUND" || failure.Code == "INVALID_PARAM")) {
		return m, err
	}
	if err != nil || !m.Enabled {
		if recent {
			if err != nil {
				return m, fail("NOT_FOUND", "最近运行的脚本已删除")
			}
			return m, fail("DISABLED", "最近运行的脚本已禁用")
		}
		if err := state.Set(l.db, "lastSelectedScriptId", nil); err != nil {
			return m, err
		}
		return m, fail("SELECTION_DISABLED", "选中的脚本已删除或禁用")
	}
	return m, nil
}

func (l *Library) Metadata(id string) (Meta, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.meta(id)
}
