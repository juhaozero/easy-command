package sched

import (
	"encoding/json"
	"errors"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"easy-command/sidecar/internal/store"
	"github.com/robfig/cron/v3"
)

type Error struct{ Code, Message string }

func (e *Error) Error() string { return e.Message }

type Rule struct {
	ID       string                     `json:"id"`
	ScriptID string                     `json:"scriptId"`
	Enabled  bool                       `json:"enabled"`
	Rule     Timing                     `json:"rule"`
	Args     map[string]json.RawMessage `json:"argsOverlay,omitempty"`
}
type Timing struct {
	Type         string `json:"type"`
	Expr         string `json:"expr,omitempty"`
	EverySeconds int    `json:"everySeconds,omitempty"`
	At           string `json:"at,omitempty"`
}

type entry struct {
	rule     Rule
	next     time.Time
	schedule cron.Schedule
}

type Scheduler struct {
	db               *store.Store
	mu               sync.Mutex
	rules            map[string]entry
	paused, stopping bool
}

var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func compile(t Timing) (cron.Schedule, error) {
	switch t.Type {
	case "cron":
		if len(strings.Fields(t.Expr)) == 5 {
			if c, err := parser.Parse(t.Expr); err == nil {
				return c, nil
			}
		}
		return nil, &Error{"INVALID_CRON", "请输入标准五段 cron 表达式"}
	case "interval":
		if t.EverySeconds >= 10 && t.EverySeconds <= 86400 {
			return nil, nil
		}
		return nil, &Error{"INVALID_INTERVAL", "间隔必须为 10 至 86400 秒"}
	case "daily":
		if at, err := time.Parse("15:04", t.At); err == nil && at.Format("15:04") == t.At {
			return nil, nil
		}
		return nil, &Error{"INVALID_DAILY", "每日时刻必须为 HH:mm"}
	}
	return nil, &Error{"INVALID_PARAM", "未知调度类型"}
}

func (e entry) after(now time.Time) time.Time {
	switch e.rule.Rule.Type {
	case "cron":
		return e.schedule.Next(now)
	case "interval":
		return now.Add(time.Duration(e.rule.Rule.EverySeconds) * time.Second)
	default:
		at, _ := time.Parse("15:04", e.rule.Rule.At)
		next := time.Date(now.Year(), now.Month(), now.Day(), at.Hour(), at.Minute(), 0, 0, now.Location())
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		return next
	}
}

func Open(db *store.Store, now time.Time) (*Scheduler, error) {
	s := &Scheduler{db: db, rules: make(map[string]entry)}
	var settings struct {
		Paused bool `json:"schedPaused"`
	}
	if err := db.ReadJSON(db.Path("config", "settings.json"), &settings); err != nil {
		return nil, err
	}
	s.paused = settings.Paused
	var rules []Rule
	if err := db.ReadJSON(db.Path("config", "schedules.json"), &rules); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	for _, rule := range rules {
		schedule, err := compile(rule.Rule)
		if err != nil {
			return nil, err
		}
		e := entry{rule: rule, schedule: schedule}
		e.next = e.after(now)
		s.rules[rule.ID] = e
	}
	return s, nil
}

func (s *Scheduler) list() []Rule {
	rules := make([]Rule, 0, len(s.rules))
	for _, e := range s.rules {
		rules = append(rules, e.rule)
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })
	return rules
}

func (s *Scheduler) List() ([]Rule, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list(), s.paused
}

func (s *Scheduler) Upsert(rule Rule, now time.Time) error {
	schedule, err := compile(rule.Rule)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, exists := s.rules[rule.ID]
	e := entry{rule: rule, schedule: schedule}
	e.next = e.after(now)
	s.rules[rule.ID] = e
	if err := s.save(); err != nil {
		if exists {
			s.rules[rule.ID] = old
		} else {
			delete(s.rules, rule.ID)
		}
		return err
	}
	return nil
}

func (s *Scheduler) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.rules[id]
	if !ok {
		return &Error{"NOT_FOUND", "调度规则不存在"}
	}
	delete(s.rules, id)
	if err := s.save(); err != nil {
		s.rules[id] = old
		return err
	}
	return nil
}

func (s *Scheduler) save() error {
	rules := s.list()
	data, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	if len(data) > 768<<10 {
		return &Error{"SIZE_LIMIT", "调度规则总大小超过 768 KiB"}
	}
	return s.db.Write(s.db.Path("config", "schedules.json"), append(data, '\n'))
}

func (s *Scheduler) Pause(paused bool, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.paused == paused {
		return nil
	}
	if err := s.db.UpdateJSONObject(s.db.Path("config", "settings.json"), "schedPaused", paused); err != nil {
		return err
	}
	s.paused = paused
	if !paused {
		for id, e := range s.rules {
			if !e.next.After(now) {
				e.next = e.after(now)
				s.rules[id] = e
			}
		}
	}
	return nil
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopping = true
}

func (s *Scheduler) Tick(now time.Time, fire func(Rule)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopping {
		return
	}
	for _, r := range s.list() {
		e := s.rules[r.ID]
		if !r.Enabled || e.next.IsZero() || e.next.After(now) {
			continue
		}
		e.next = e.after(now)
		s.rules[r.ID] = e
		if !s.paused {
			fire(r)
		}
	}
}
