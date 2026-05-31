package main

import (
	"os"
	"path/filepath"
	"strings"
)

type Skill struct {
	Name        string
	Description string
	Triggers    []string
	Content     string
}

type SkillStore struct {
	dir    string
	skills map[string]Skill
}

func NewSkillStore(dir string) *SkillStore {
	return &SkillStore{dir: dir, skills: map[string]Skill{}}
}

func (s *SkillStore) Load() error {
	s.skills = map[string]Skill{}
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		path := filepath.Join(s.dir, ent.Name(), "SKILL.md")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		skill := parseSkill(ent.Name(), string(data))
		s.skills[strings.ToLower(skill.Name)] = skill
	}
	return nil
}

func (s *SkillStore) All() []Skill {
	out := make([]Skill, 0, len(s.skills))
	for _, skill := range s.skills {
		out = append(out, skill)
	}
	return out
}

func (s *SkillStore) Get(name string) (Skill, bool) {
	v, ok := s.skills[strings.ToLower(name)]
	return v, ok
}

func (s *SkillStore) Match(prompt string) []Skill {
	prompt = strings.ToLower(prompt)
	var out []Skill
	for _, skill := range s.skills {
		if strings.Contains(prompt, strings.ToLower(skill.Name)) {
			out = append(out, skill)
			continue
		}
		for _, trigger := range skill.Triggers {
			if trigger != "" && strings.Contains(prompt, strings.ToLower(trigger)) {
				out = append(out, skill)
				break
			}
		}
	}
	return out
}

func parseSkill(defaultName, content string) Skill {
	skill := Skill{Name: defaultName, Content: content}
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "name:"):
			skill.Name = strings.TrimSpace(line[len("name:"):])
		case strings.HasPrefix(lower, "description:"):
			skill.Description = strings.TrimSpace(line[len("description:"):])
		case strings.HasPrefix(lower, "triggers:"):
			raw := strings.TrimSpace(line[len("triggers:"):])
			for _, item := range strings.Split(raw, ",") {
				if t := strings.TrimSpace(item); t != "" {
					skill.Triggers = append(skill.Triggers, t)
				}
			}
		}
	}
	if skill.Description == "" {
		for _, line := range strings.Split(content, "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "#"))
			if line != "" && !strings.Contains(line, ":") {
				skill.Description = line
				break
			}
		}
	}
	return skill
}
