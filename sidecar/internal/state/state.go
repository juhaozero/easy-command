package state

import (
	"encoding/json"
	"errors"
	"os"

	"easy-command/sidecar/internal/store"
)

type State struct {
	Selected *string `json:"lastSelectedScriptId"`
	Recent   *string `json:"lastRunScriptId"`
}

func path(db *store.Store) string { return db.Path("state", "ui_state.json") }

func Get(db *store.Store) (State, error) {
	var current State
	err := db.ReadJSON(path(db), &current)
	if errors.Is(err, os.ErrNotExist) {
		err = nil
	}
	return current, err
}

// Call mutations under the library lock so validation and state updates are serialized.
func Set(db *store.Store, field string, id *string) error {
	fields := make(map[string]json.RawMessage)
	if err := db.ReadJSON(path(db), &fields); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if fields == nil {
		fields = make(map[string]json.RawMessage)
	}
	value, err := json.Marshal(id)
	if err != nil {
		return err
	}
	fields[field] = value
	for _, key := range []string{"lastSelectedScriptId", "lastRunScriptId"} {
		if fields[key] == nil {
			fields[key] = json.RawMessage("null")
		}
	}
	return db.WriteJSON(path(db), fields)
}
