package history

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"sync"
	"time"

	"easy-command/sidecar/internal/logretain"
	"easy-command/sidecar/internal/script"
	"easy-command/sidecar/internal/store"
)

const maxRecord = 256 << 10
const listBudget = 640 << 10

var ErrInvalid = errors.New("invalid history query or retention settings")
var ErrNotFound = errors.New("run not found")

type Record struct {
	ID         string                     `json:"id"`
	ScriptID   string                     `json:"scriptId"`
	ScriptName string                     `json:"scriptName"`
	StartedAt  time.Time                  `json:"startedAt"`
	EndedAt    *time.Time                 `json:"endedAt,omitempty"`
	ExitCode   *uint32                    `json:"exitCode"`
	Status     string                     `json:"status"`
	ReasonCode string                     `json:"reasonCode,omitempty"`
	Trigger    string                     `json:"trigger"`
	ArgsMasked map[string]json.RawMessage `json:"argsMasked"`
	LogRef     string                     `json:"logRef,omitempty"`
}

type History struct {
	db         *store.Store
	logs       *logretain.Logs
	diagnostic io.Writer
	mu         sync.Mutex
	latest     map[string]Record
	active     map[string]string
	loadErr    error
}

func New(db *store.Store, diagnostic io.Writer) *History {
	return &History{db: db, logs: logretain.New(db), diagnostic: diagnostic, latest: make(map[string]Record), active: make(map[string]string)}
}

func (h *History) path() string { return h.db.Path("history", "runs.jsonl") }

func (h *History) Recover(now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.loadErr = h.load()
	if h.loadErr != nil {
		h.report("history recovery", h.loadErr)
		return
	}
	for _, r := range h.latest {
		if r.Status != "running" {
			continue
		}
		r.Status, r.EndedAt, r.ExitCode = "interrupted", &now, nil
		var logErr error
		if r.LogRef != "" {
			logErr = h.logs.Append(r.LogRef, logretain.Entry{RunID: r.ID, Kind: "interrupted", Time: now, Data: map[string]any{"status": r.Status, "exitCode": nil, "message": "Sidecar 重启时发现此执行仍在运行"}})
		}
		h.report("interrupted record", errors.Join(h.append(r), logErr))
	}
}

func (h *History) load() error {
	path := h.path()
	if err := store.RepairAppendTail(path); err != nil {
		return err
	}
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer file.Close()
	latest := make(map[string]Record)
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 4096), maxRecord)
	for scan.Scan() {
		var r Record
		if err := json.Unmarshal(scan.Bytes(), &r); err != nil {
			return err
		}
		if r.ID == "" || r.StartedAt.IsZero() {
			return errors.New("invalid run record")
		}
		latest[r.ID] = r
	}
	if err := scan.Err(); err != nil {
		return err
	}
	h.latest = latest
	return nil
}

func (h *History) append(r Record) error {
	if h.loadErr != nil {
		return h.loadErr
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(data) >= maxRecord {
		return errors.New("history record exceeds size limit")
	}
	if err := h.db.AppendLine(h.path(), data, true); err != nil {
		return err
	}
	h.latest[r.ID] = r
	return nil
}

func Snapshot(params []script.Param, values map[string]json.RawMessage) map[string]json.RawMessage {
	result := make(map[string]json.RawMessage)
	for _, param := range params {
		raw, provided := values[param.Key]
		if !provided {
			raw = param.Default
		}
		if param.Type == "password" || param.MaskInLog {
			result[param.Key] = json.RawMessage(`"***"`)
		} else if len(raw) > 0 {
			result[param.Key] = append(json.RawMessage(nil), raw...)
		}
	}
	return result
}

func (h *History) Begin(r Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.active[r.ID] = r.LogRef
	return errors.Join(h.append(r), h.logs.Append(r.LogRef, logretain.Entry{RunID: r.ID, Kind: "start", Time: r.StartedAt, Data: r}))
}

func (h *History) Add(r Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	err := h.append(r)
	h.report("schedule history", err)
	return err
}

func (h *History) Output(r Record, stream, text string, continued bool) error {
	return h.logs.Append(r.LogRef, logretain.Entry{RunID: r.ID, Kind: stream, Time: time.Now(), Data: map[string]any{"text": text, "continued": continued}})
}

func (h *History) Finish(r Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.active, r.ID)
	err := h.append(r)
	logErr := h.logs.Append(r.LogRef, logretain.Entry{RunID: r.ID, Kind: "end", Time: *r.EndedAt, Data: map[string]any{"status": r.Status, "exitCode": r.ExitCode}})
	return errors.Join(err, logErr)
}

type Filter struct {
	ScriptID string     `json:"scriptId"`
	Status   string     `json:"status"`
	From     *time.Time `json:"from"`
	To       *time.Time `json:"to"`
	Cursor   string     `json:"cursor"`
	Limit    int        `json:"limit"`
}

type Page struct {
	Runs       []Record `json:"runs"`
	NextCursor *string  `json:"nextCursor"`
}

func (h *History) List(f Filter) (Page, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	page := Page{Runs: []Record{}}
	if h.loadErr != nil {
		return page, h.loadErr
	}
	if f.Limit == 0 {
		f.Limit = 50
	}
	if f.Limit < 1 || f.Limit > 100 || f.From != nil && f.To != nil && !f.From.Before(*f.To) {
		return page, ErrInvalid
	}
	switch f.Status {
	case "", "running", "succeeded", "failed", "cancelled", "interrupted", "skipped":
	default:
		return page, ErrInvalid
	}
	var cursor Record
	if f.Cursor != "" {
		var exists bool
		cursor, exists = h.latest[f.Cursor]
		if !exists {
			return page, ErrInvalid
		}
	}
	before := func(a, b Record) bool {
		if a.StartedAt.Equal(b.StartedAt) {
			return a.ID < b.ID
		}
		return a.StartedAt.After(b.StartedAt)
	}
	candidates := make([]Record, 0)
	for _, r := range h.latest {
		if f.ScriptID != "" && r.ScriptID != f.ScriptID || f.Status != "" && r.Status != f.Status ||
			f.From != nil && r.StartedAt.Before(*f.From) || f.To != nil && !r.StartedAt.Before(*f.To) ||
			f.Cursor != "" && !before(cursor, r) {
			continue
		}
		candidates = append(candidates, r)
	}
	sort.Slice(candidates, func(i, j int) bool { return before(candidates[i], candidates[j]) })
	used := 0
	for _, r := range candidates {
		data, err := json.Marshal(r)
		if err != nil {
			return page, err
		}
		if len(page.Runs) == f.Limit || len(page.Runs) > 0 && used+len(data)+1 > listBudget {
			last := page.Runs[len(page.Runs)-1].ID
			page.NextCursor = &last
			break
		}
		page.Runs = append(page.Runs, r)
		used += len(data) + 1
	}
	return page, nil
}

func (h *History) Get(id string, cursor *int64) (Record, *logretain.Page, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.loadErr != nil {
		return Record{}, nil, h.loadErr
	}
	r, ok := h.latest[id]
	if !ok {
		return r, nil, ErrNotFound
	}
	if cursor == nil || r.LogRef == "" {
		return r, nil, nil
	}
	page, err := h.logs.Read(r.LogRef, id, *cursor)
	return r, &page, err
}

func (h *History) Policy() (logretain.Policy, error) {
	var config struct {
		Retention *logretain.Policy `json:"logRetention"`
	}
	if err := h.db.ReadJSON(h.db.Path("config", "settings.json"), &config); err != nil {
		return logretain.Policy{}, err
	}
	if config.Retention == nil {
		return logretain.Policy{Mode: "keep_forever"}, nil
	}
	if !config.Retention.Valid() {
		return logretain.Policy{}, ErrInvalid
	}
	return *config.Retention, nil
}

func (h *History) SetPolicy(p logretain.Policy) error {
	if !p.Valid() {
		return ErrInvalid
	}
	return h.db.UpdateJSONObject(h.db.Path("config", "settings.json"), "logRetention", p)
}

func (h *History) Clean(now time.Time) {
	if err := h.clean(now); err != nil {
		h.report("log retention", err)
		_ = h.logs.Append("system.log", logretain.Entry{Kind: "retention_error", Time: now, Data: err.Error()})
	}
}

func (h *History) clean(now time.Time) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	p, err := h.Policy()
	if err != nil || p.Mode == "keep_forever" {
		return err
	}
	if h.loadErr != nil {
		return h.loadErr
	}
	cutoff := p.Cutoff(now)
	retained := make(map[string]Record, len(h.latest))
	protected := make(map[string]bool, len(h.active))
	for _, ref := range h.active {
		protected[ref] = true
	}
	for id, r := range h.latest {
		if _, active := h.active[id]; active || !r.StartedAt.Before(cutoff) {
			retained[id] = r
			if r.LogRef != "" {
				protected[r.LogRef] = true
			}
		}
	}
	if len(retained) != len(h.latest) {
		if err := h.db.Rewrite(h.path(), func(w io.Writer) error {
			encode := json.NewEncoder(w)
			for _, r := range retained {
				if err := encode.Encode(r); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
		h.latest = retained
	}
	return h.logs.Prune(cutoff, protected)
}

func (h *History) report(action string, err error) {
	if err != nil && h.diagnostic != nil {
		_, _ = fmt.Fprintf(h.diagnostic, "%s: %v\n", action, err)
	}
}
