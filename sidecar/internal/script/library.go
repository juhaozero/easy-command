package script

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"easy-command/sidecar/internal/store"
)

const (
	MaxContent = 2 << 20
	ChunkSize  = 256 << 10
	maxMeta    = 128 << 10
)

type Error struct{ Code, Message string }

func (e *Error) Error() string        { return e.Message }
func fail(code, message string) error { return &Error{code, message} }

type Library struct {
	mu      sync.Mutex
	db      *store.Store
	uploads map[string]*upload
}

func Open(db *store.Store) (*Library, error) {
	l := &Library{db: db, uploads: make(map[string]*upload)}
	if err := l.recover(); err != nil {
		return nil, err
	}
	return l, nil
}

func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

func validID(id string) bool {
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return false
	}
	raw := strings.ReplaceAll(id, "-", "")
	_, err := hex.DecodeString(raw)
	return len(raw) == 32 && err == nil && strings.ToLower(id) == id
}

func (l *Library) dir(id string) string { return l.db.Path("library", "scripts", id) }

func (l *Library) Exists(id string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err := l.meta(id)
	var failure *Error
	if errors.As(err, &failure) && failure.Code == "NOT_FOUND" {
		return false, nil
	}
	return err == nil, err
}

func (l *Library) Executable(id string) (Meta, string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	meta, err := l.meta(id)
	if err != nil {
		return meta, "", err
	}
	if !meta.Enabled {
		return meta, "", fail("SCRIPT_DISABLED", "脚本已禁用")
	}
	path := filepath.Join(l.dir(id), meta.FileName)
	return meta, path, store.CheckPath(path)
}

func (l *Library) meta(id string) (Meta, error) {
	var m Meta
	if !validID(id) {
		return m, fail("INVALID_PARAM", "脚本 ID 无效")
	}
	if err := l.checkRoot(); err != nil {
		return m, err
	}
	if err := store.CheckPath(l.dir(id)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m, fail("NOT_FOUND", "脚本不存在")
		}
		return m, err
	}
	var disk storedMeta
	if err := l.db.ReadJSON(filepath.Join(l.dir(id), "meta.json"), &disk); err != nil {
		return m, err
	}
	m = disk.Meta
	m.secrets = disk.Secrets
	if m.ID != id || (m.Kind != "bat" && m.Kind != "sh") || m.FileName != "script."+m.Kind {
		return m, errors.New("invalid stored script metadata")
	}
	if m.Params == nil {
		m.Params = []Param{}
	}
	m.applyPasswordStatus()
	return m, nil
}

func (l *Library) index() ([]Summary, error) {
	if err := l.checkRoot(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(l.dir(""))
	if err != nil {
		return nil, err
	}
	items := make([]Summary, 0)
	for _, entry := range entries {
		if !validID(entry.Name()) {
			continue
		}
		m, err := l.meta(entry.Name())
		if err != nil {
			return nil, err
		}
		items = append(items, Summary{m.ID, m.Name, m.Enabled, m.Dangerous, m.UpdatedAt})
	}
	return items, nil
}

func (l *Library) checkRoot() error {
	for _, path := range []string{l.db.Root, l.db.Path("library"), l.dir("")} {
		if err := store.CheckPath(path); err != nil {
			return err
		}
	}
	return nil
}

func (l *Library) rebuild() error {
	items, err := l.index()
	if err != nil {
		return err
	}
	return l.db.WriteJSON(l.db.Path("library", "index.json"), items)
}

func (l *Library) List(after string, limit int) ([]Summary, *string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit == 0 {
		limit = 100
	}
	if limit < 1 || limit > 100 || (after != "" && !validID(after)) {
		return nil, nil, fail("INVALID_PARAM", "列表游标或页大小无效")
	}
	items, err := l.index()
	if err != nil {
		return nil, nil, err
	}
	start := sort.Search(len(items), func(i int) bool { return items[i].ID > after })
	end := min(start+limit, len(items))
	var next *string
	if end < len(items) {
		next = &items[end-1].ID
	}
	return items[start:end], next, nil
}

func (l *Library) Import(source, policy string) (Meta, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	var m Meta
	kind := strings.TrimPrefix(strings.ToLower(filepath.Ext(source)), ".")
	if !filepath.IsAbs(source) || (kind != "bat" && kind != "sh") || (policy != "rename" && policy != "overwrite" && policy != "cancel") {
		return m, fail("INVALID_PARAM", "请输入 .bat 或 .sh 的绝对路径及有效冲突策略")
	}
	relative, err := filepath.Rel(l.dir(""), filepath.Clean(source))
	if err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return m, fail("INVALID_PARAM", "请选择库外的源文件")
	}
	file, err := os.Open(source)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m, fail("NOT_FOUND", "源文件不存在")
		}
		return m, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return m, err
	}
	if !info.Mode().IsRegular() {
		return m, fail("INVALID_PARAM", "源路径必须指向普通文件")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxContent+1))
	if err != nil {
		return m, err
	}
	if len(data) > MaxContent {
		return m, fail("SIZE_LIMIT", "脚本不能超过 2 MiB")
	}
	items, err := l.index()
	if err != nil {
		return m, err
	}
	name := filepath.Base(source)
	conflict := findName(items, name)
	if conflict != "" {
		switch policy {
		case "cancel":
			return m, fail("CONFLICT_CANCELLED", "已有同名脚本，导入已取消")
		case "rename":
			ext := filepath.Ext(name)
			stem := strings.TrimSuffix(name, ext)
			for n := 2; ; n++ {
				name = fmt.Sprintf("%s (%d)%s", stem, n, ext)
				if findName(items, name) == "" {
					break
				}
			}
			conflict = ""
		}
	}
	now := timestamp()
	fields := Fields{Name: name, Enabled: true, Params: []Param{}}
	if err := fields.validate(); err != nil {
		return m, err
	}
	id := conflict
	created := now
	if id == "" {
		id, err = NewID()
		if err != nil {
			return m, err
		}
	} else {
		old, err := l.meta(id)
		if err != nil {
			return m, err
		}
		oldPath := filepath.Join(l.dir(id), old.FileName)
		if err := store.CheckPath(oldPath); err != nil {
			return m, err
		}
		oldInfo, err := os.Stat(oldPath)
		if err != nil {
			return m, err
		}
		if os.SameFile(info, oldInfo) {
			return m, fail("INVALID_PARAM", "请选择库外的源文件")
		}
		created = old.CreatedAt
	}
	if err := file.Close(); err != nil {
		return m, err
	}
	m = Meta{Fields: fields, ID: id, FileName: "script." + kind, Kind: kind, CreatedAt: created, UpdatedAt: now}
	if err := l.publish(m, data, conflict != ""); err != nil {
		return m, err
	}
	return m, l.rebuild()
}

func findName(items []Summary, name string) string {
	for _, item := range items {
		if strings.EqualFold(item.Name, name) {
			return item.ID
		}
	}
	return ""
}

func (l *Library) UpdateMeta(id string, fields Fields) (Meta, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	m, err := l.meta(id)
	if err != nil {
		return m, false, err
	}
	if err := fields.validate(); err != nil {
		return m, false, err
	}
	items, err := l.index()
	if err != nil {
		return m, false, err
	}
	if other := findName(items, fields.Name); other != "" && other != id {
		return m, false, fail("INVALID_PARAM", "脚本名称已存在")
	}
	m.Fields = fields
	bindings := make(map[string]bool)
	for _, p := range m.Params {
		if p.Type == "password" {
			bindings[binding(p)] = true
		}
	}
	kept := m.secrets[:0]
	for _, entry := range m.secrets {
		if bindings[entry.Key] {
			kept = append(kept, entry)
		}
	}
	m.secrets = kept
	m.applyPasswordStatus()
	m.UpdatedAt = timestamp()
	if err := l.writeMeta(l.dir(id), m); err != nil {
		return m, false, err
	}
	var selected bool
	if !fields.Enabled {
		selected, _, err = l.clearState(id, false)
		if err != nil {
			return m, false, err
		}
	}
	return m, selected, l.rebuild()
}

func (l *Library) Delete(id string) (bool, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, err := l.meta(id); err != nil {
		return false, false, err
	}
	trash := l.dir(".delete-" + id)
	if err := store.Move(l.dir(id), trash); err != nil {
		return false, false, err
	}
	delete(l.uploads, id)
	selected, last, err := l.clearState(id, true)
	if err != nil {
		return false, false, err
	}
	if err := l.rebuild(); err != nil {
		return false, false, err
	}
	return selected, last, os.RemoveAll(trash)
}

func (l *Library) clearState(id string, clearLast bool) (bool, bool, error) {
	path := l.db.Path("state", "ui_state.json")
	if err := store.CheckPath(filepath.Dir(path)); err != nil {
		return false, false, err
	}
	state := make(map[string]json.RawMessage)
	if err := l.db.ReadJSON(path, &state); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, false, nil
		}
		return false, false, err
	}
	matches := func(key string) bool {
		var value string
		return json.Unmarshal(state[key], &value) == nil && value == id
	}
	selected, last := matches("lastSelectedScriptId"), clearLast && matches("lastRunScriptId")
	if selected {
		state["lastSelectedScriptId"] = json.RawMessage("null")
	}
	if last {
		state["lastRunScriptId"] = json.RawMessage("null")
	}
	if selected || last {
		return selected, last, l.db.WriteJSON(path, state)
	}
	return false, false, nil
}

func (l *Library) publish(m Meta, data []byte, overwrite bool) error {
	stage, err := os.MkdirTemp(l.dir(""), ".stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := l.db.Write(filepath.Join(stage, m.FileName), data); err != nil {
		return err
	}
	if err := l.writeMeta(stage, m); err != nil {
		return err
	}
	target := l.dir(m.ID)
	backup := l.dir(".replace-" + m.ID)
	if overwrite {
		if err := store.Move(target, backup); err != nil {
			return err
		}
	}
	if err := store.Move(stage, target); err != nil {
		if overwrite {
			return errors.Join(err, store.Move(backup, target))
		}
		return err
	}
	if overwrite {
		return os.RemoveAll(backup)
	}
	return nil
}

func (l *Library) recover() error {
	entries, err := os.ReadDir(l.dir(""))
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		path := l.dir(name)
		switch {
		case strings.HasPrefix(name, ".stage-"):
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		case strings.HasPrefix(name, ".replace-") && validID(strings.TrimPrefix(name, ".replace-")):
			id := strings.TrimPrefix(name, ".replace-")
			if _, err := os.Lstat(l.dir(id)); errors.Is(err, os.ErrNotExist) {
				if err := store.Move(path, l.dir(id)); err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if err := os.RemoveAll(path); err != nil {
				return err
			}
		case strings.HasPrefix(name, ".delete-") && validID(strings.TrimPrefix(name, ".delete-")):
			if _, _, err := l.clearState(strings.TrimPrefix(name, ".delete-"), true); err != nil {
				return err
			}
			if err := os.RemoveAll(path); err != nil {
				return err
			}
		}
	}
	items, err := l.index()
	if err != nil {
		return err
	}
	for _, item := range items {
		if !item.Enabled {
			if _, _, err := l.clearState(item.ID, false); err != nil {
				return err
			}
		}
	}
	return l.db.WriteJSON(l.db.Path("library", "index.json"), items)
}
