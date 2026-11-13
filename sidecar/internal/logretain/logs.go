package logretain

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"easy-command/sidecar/internal/store"
)

const DateLayout = "2006-01-02"
const pageBudget = 384 << 10
const scanBudget = 4 << 20

type Policy struct {
	Mode string `json:"mode"`
	Days int    `json:"days,omitempty"`
}

func (p Policy) Valid() bool {
	return p.Mode == "keep_forever" && p.Days == 0 || p.Mode == "days" && p.Days >= 1 && p.Days <= 3650
}

func (p Policy) Cutoff(now time.Time) time.Time {
	y, m, d := now.Local().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.Local).AddDate(0, 0, -p.Days)
}

type Entry struct {
	RunID string    `json:"runId,omitempty"`
	Kind  string    `json:"kind"`
	Time  time.Time `json:"ts"`
	Data  any       `json:"data"`
}

type Page struct {
	Entries    []json.RawMessage `json:"entries"`
	NextCursor *int64            `json:"nextCursor"`
}

type Logs struct {
	db       *store.Store
	mu       sync.Mutex
	prepared map[string]bool
}

func New(db *store.Store) *Logs {
	return &Logs{db: db, prepared: make(map[string]bool)}
}

func (l *Logs) Append(ref string, entry Entry) error {
	if ref != "system.log" && !validRef(ref) {
		return errors.New("invalid log reference")
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	path := l.db.Path("logs", ref)
	if !l.prepared[ref] {
		if err := store.RepairAppendTail(path); err != nil {
			return err
		}
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if len(data) >= pageBudget {
		return errors.New("log entry exceeds size limit")
	}
	err = l.db.AppendLine(path, data, false)
	l.prepared[ref] = err == nil
	return err
}

func (l *Logs) Read(ref, id string, cursor int64) (Page, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	page := Page{Entries: []json.RawMessage{}}
	if !validRef(ref) {
		return page, errors.New("invalid log reference")
	}
	path := l.db.Path("logs", ref)
	if err := store.CheckPath(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return page, nil
		}
		return page, err
	}
	file, err := os.Open(path)
	if err != nil {
		return page, err
	}
	defer file.Close()
	size, err := file.Seek(0, io.SeekEnd)
	if err != nil {
		return page, err
	}
	if cursor < 0 || cursor > size {
		return page, ErrCursor
	}
	if cursor > 0 {
		var previous [1]byte
		if _, err := file.ReadAt(previous[:], cursor-1); err != nil {
			return page, err
		}
		if previous[0] != '\n' {
			return page, ErrCursor
		}
	}
	if _, err := file.Seek(cursor, io.SeekStart); err != nil {
		return page, err
	}
	scan := bufio.NewScanner(file)
	scan.Buffer(make([]byte, 8192), pageBudget)
	offset := cursor
	used := 0
	for scan.Scan() {
		line := scan.Bytes()
		var envelope struct {
			RunID string `json:"runId"`
		}
		matches := json.Unmarshal(line, &envelope) == nil && envelope.RunID == id
		if matches {
			if used+len(line)+1 > pageBudget && len(page.Entries) > 0 {
				page.NextCursor = &offset
				return page, nil
			}
			page.Entries = append(page.Entries, append(json.RawMessage(nil), line...))
			used += len(line) + 1
		}
		offset += int64(len(line)) + 1
		if offset-cursor >= scanBudget && offset < size {
			page.NextCursor = &offset
			return page, nil
		}
	}
	return page, scan.Err()
}

var ErrCursor = errors.New("invalid log cursor")

func (l *Logs) Prune(cutoff time.Time, protected map[string]bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, err := os.ReadDir(l.db.Path("logs"))
	if err != nil {
		return err
	}
	var result error
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || protected[name] || !validRef(name) || name >= "exec-"+cutoff.Format(DateLayout)+".log" {
			continue
		}
		path := l.db.Path("logs", name)
		if err := store.CheckPath(path); err != nil {
			result = errors.Join(result, err)
			continue
		}
		if err := os.Remove(path); err != nil {
			result = errors.Join(result, err)
			continue
		}
		delete(l.prepared, name)
	}
	return result
}

func validRef(name string) bool {
	date, ok := strings.CutPrefix(name, "exec-")
	if !ok {
		return false
	}
	date, ok = strings.CutSuffix(date, ".log")
	if !ok {
		return false
	}
	_, err := time.Parse(DateLayout, date)
	return err == nil
}
