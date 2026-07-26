package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/DaikonSushi/bot-platform/pkg/pluginsdk"
)

type ScheduledTask struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	Prompt     string    `json:"prompt"`
	TargetType string    `json:"target_type"`
	TargetID   int64     `json:"target_id"`
	Schedule   string    `json:"schedule"`
	Enabled    bool      `json:"enabled"`
	NextRunAt  time.Time `json:"next_run_at"`
	LastRunAt  time.Time `json:"last_run_at"`
	CreatedAt  time.Time `json:"created_at"`
}

type Scheduler struct {
	cfg    *Config
	state  *State
	bot    *pluginsdk.BotClient
	engine *AgentEngine
	stop   chan struct{}
}

func NewScheduler(cfg *Config, state *State, bot *pluginsdk.BotClient) *Scheduler {
	return &Scheduler{cfg: cfg, state: state, bot: bot, stop: make(chan struct{})}
}

func (s *Scheduler) SetEngine(engine *AgentEngine) {
	s.engine = engine
}

func (s *Scheduler) Start() {
	interval := time.Duration(s.cfg.Schedule.CheckSeconds) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.tick()
			case <-s.stop:
				return
			}
		}
	}()
}

func (s *Scheduler) Stop() {
	close(s.stop)
}

func (s *Scheduler) Add(name, prompt, targetType string, targetID int64, schedule string) (*ScheduledTask, error) {
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}
	if targetType != "group" && targetType != "private" {
		return nil, fmt.Errorf("target_type must be group or private")
	}
	next, err := NextRun(schedule, time.Now().In(s.cfg.Location()), s.cfg.Location())
	if err != nil {
		return nil, err
	}
	task := ScheduledTask{
		ID:         fmt.Sprintf("task_%d", time.Now().UnixNano()),
		Name:       name,
		Prompt:     prompt,
		TargetType: targetType,
		TargetID:   targetID,
		Schedule:   schedule,
		Enabled:    true,
		NextRunAt:  next,
		CreatedAt:  time.Now(),
	}
	if task.Name == "" {
		task.Name = shortText(prompt, 32)
	}
	s.state.mu.Lock()
	s.state.Tasks = append(s.state.Tasks, task)
	err = s.state.saveLocked()
	s.state.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *Scheduler) Delete(id string) bool {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	for i, task := range s.state.Tasks {
		if task.ID == id {
			s.state.Tasks = append(s.state.Tasks[:i], s.state.Tasks[i+1:]...)
			_ = s.state.saveLocked()
			return true
		}
	}
	return false
}

func (s *Scheduler) SetEnabled(id string, enabled bool) bool {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	for i := range s.state.Tasks {
		if s.state.Tasks[i].ID == id {
			s.state.Tasks[i].Enabled = enabled
			_ = s.state.saveLocked()
			return true
		}
	}
	return false
}

func (s *Scheduler) List() []ScheduledTask {
	s.state.mu.Lock()
	defer s.state.mu.Unlock()
	out := make([]ScheduledTask, len(s.state.Tasks))
	copy(out, s.state.Tasks)
	return out
}

func (s *Scheduler) tick() {
	now := time.Now().In(s.cfg.Location())
	var due []ScheduledTask
	s.state.mu.Lock()
	for i := range s.state.Tasks {
		task := &s.state.Tasks[i]
		if task.Enabled && !task.NextRunAt.IsZero() && !task.NextRunAt.After(now) {
			due = append(due, *task)
			task.LastRunAt = now
			next, err := NextRun(task.Schedule, now.Add(time.Second), s.cfg.Location())
			if err != nil || isOneShotSchedule(task.Schedule) {
				task.Enabled = false
			} else {
				task.NextRunAt = next
			}
		}
	}
	_ = s.state.saveLocked()
	s.state.mu.Unlock()
	for _, task := range due {
		go s.runTask(task)
	}
}

func (s *Scheduler) runTask(task ScheduledTask) {
	if s.engine == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.Model.TimeoutSec)*time.Second)
	defer cancel()
	key := task.TargetType + ":" + formatInt(task.TargetID)
	result, err := s.engine.Run(ctx, key, task.Prompt)
	if err != nil {
		result = "Scheduled task failed: " + err.Error()
	}
	if task.TargetType == "group" {
		_, _ = s.bot.SendGroupMessage(task.TargetID, pluginsdk.Text(result))
	} else {
		_, _ = s.bot.SendPrivateMessage(task.TargetID, pluginsdk.Text(result))
	}
}

func NextRun(spec string, now time.Time, loc *time.Location) (time.Time, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return time.Time{}, fmt.Errorf("schedule is required")
	}
	if isDurationSchedule(spec) {
		d, err := time.ParseDuration(spec)
		if err != nil {
			return time.Time{}, err
		}
		return now.Add(d), nil
	}
	if strings.HasPrefix(spec, "once:") {
		return parseOnce(strings.TrimPrefix(spec, "once:"), loc)
	}
	if strings.HasPrefix(spec, "daily@") {
		hm := strings.TrimPrefix(spec, "daily@")
		t, err := parseHourMinute(hm, now, loc)
		if err != nil {
			return time.Time{}, err
		}
		if !t.After(now) {
			t = t.Add(24 * time.Hour)
		}
		return t, nil
	}
	if strings.HasPrefix(spec, "weekly@") {
		parts := strings.Split(strings.TrimPrefix(spec, "weekly@"), ",")
		if len(parts) != 2 {
			return time.Time{}, fmt.Errorf("weekly schedule must be weekly@Mon,09:30")
		}
		target, ok := parseWeekday(parts[0])
		if !ok {
			return time.Time{}, fmt.Errorf("invalid weekday")
		}
		t, err := parseHourMinute(parts[1], now, loc)
		if err != nil {
			return time.Time{}, err
		}
		days := (int(target) - int(now.Weekday()) + 7) % 7
		t = t.AddDate(0, 0, days)
		if !t.After(now) {
			t = t.AddDate(0, 0, 7)
		}
		return t, nil
	}
	return parseOnce(spec, loc)
}

func isDurationSchedule(spec string) bool {
	if spec == "" || strings.Contains(spec, "-") || strings.Contains(spec, ":") || strings.Contains(spec, "@") {
		return false
	}
	_, err := time.ParseDuration(spec)
	return err == nil
}

func isOneShotSchedule(spec string) bool {
	spec = strings.TrimSpace(spec)
	if spec == "" || strings.HasPrefix(spec, "daily@") || strings.HasPrefix(spec, "weekly@") {
		return false
	}
	if strings.HasPrefix(spec, "once:") || isDurationSchedule(spec) {
		return true
	}
	_, err := parseOnce(spec, time.Local)
	return err == nil
}

func parseOnce(value string, loc *time.Location) (time.Time, error) {
	value = strings.TrimSpace(value)
	layouts := []string{time.RFC3339, "2006-01-02 15:04", "2006-01-02 15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported schedule: %s", value)
}

func parseHourMinute(hm string, now time.Time, loc *time.Location) (time.Time, error) {
	t, err := time.ParseInLocation("15:04", hm, loc)
	if err != nil {
		return time.Time{}, err
	}
	return time.Date(now.Year(), now.Month(), now.Day(), t.Hour(), t.Minute(), 0, 0, loc), nil
}

func parseWeekday(s string) (time.Weekday, bool) {
	names := map[string]time.Weekday{
		"sun": time.Sunday, "sunday": time.Sunday,
		"mon": time.Monday, "monday": time.Monday,
		"tue": time.Tuesday, "tuesday": time.Tuesday,
		"wed": time.Wednesday, "wednesday": time.Wednesday,
		"thu": time.Thursday, "thursday": time.Thursday,
		"fri": time.Friday, "friday": time.Friday,
		"sat": time.Saturday, "saturday": time.Saturday,
	}
	v, ok := names[strings.ToLower(strings.TrimSpace(s))]
	return v, ok
}
