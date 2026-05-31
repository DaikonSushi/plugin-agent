package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type AgentEngine struct {
	cfg    *Config
	state  *State
	tools  *ToolRegistry
	client *http.Client
}

type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolDef struct {
	Type     string          `json:"type"`
	Function ToolDefFunction `json:"function"`
}

type ToolDefFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

func NewAgentEngine(cfg *Config, state *State, tools *ToolRegistry) *AgentEngine {
	return &AgentEngine{
		cfg:   cfg,
		state: state,
		tools: tools,
		client: &http.Client{
			Timeout: time.Duration(cfg.Model.TimeoutSec) * time.Second,
		},
	}
}

func (e *AgentEngine) Run(ctx context.Context, key, prompt string) (string, error) {
	messages := []ChatMessage{{Role: "system", Content: e.systemPrompt(prompt)}}
	for _, rec := range e.state.Conversation(key) {
		messages = append(messages, ChatMessage{Role: rec.Role, Content: rec.Content})
	}
	messages = append(messages, ChatMessage{Role: "user", Content: prompt})

	maxSteps := e.cfg.Model.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 8
	}
	var final string
	for step := 0; step < maxSteps; step++ {
		msg, err := e.chat(ctx, messages)
		if err != nil {
			return "", err
		}
		messages = append(messages, msg)
		if len(msg.ToolCalls) == 0 {
			final = strings.TrimSpace(msg.Content)
			break
		}
		for _, call := range msg.ToolCalls {
			result := e.tools.Execute(ctx, call.Function.Name, call.Function.Arguments)
			content := result.Content
			if result.Error != "" {
				content = "ERROR: " + result.Error
			}
			messages = append(messages, ChatMessage{
				Role:       "tool",
				ToolCallID: call.ID,
				Name:       call.Function.Name,
				Content:    content,
			})
		}
	}
	if final == "" {
		final = "Task stopped after reaching the configured tool step limit."
	}
	_ = e.state.AddConversation(key, "user", prompt)
	_ = e.state.AddConversation(key, "assistant", final)
	return final, nil
}

func (e *AgentEngine) chat(ctx context.Context, messages []ChatMessage) (ChatMessage, error) {
	apiKey := os.Getenv(e.cfg.Model.APIKeyEnv)
	if apiKey == "" {
		return ChatMessage{}, fmt.Errorf("missing API key env %s", e.cfg.Model.APIKeyEnv)
	}
	reqBody := map[string]any{
		"model":       e.cfg.Model.Model,
		"messages":    messages,
		"temperature": e.cfg.Model.Temperature,
		"tools":       e.tools.Definitions(),
		"tool_choice": "auto",
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return ChatMessage{}, err
	}
	url := strings.TrimRight(e.cfg.Model.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return ChatMessage{}, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := e.client.Do(req)
	if err != nil {
		return ChatMessage{}, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return ChatMessage{}, fmt.Errorf("model request failed: %s: %s", resp.Status, shortText(string(respBody), 800))
	}
	var data struct {
		Choices []struct {
			Message ChatMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &data); err != nil {
		return ChatMessage{}, err
	}
	if len(data.Choices) == 0 {
		return ChatMessage{}, errors.New("model returned no choices")
	}
	return data.Choices[0].Message, nil
}

func (e *AgentEngine) systemPrompt(userPrompt string) string {
	skills := e.tools.skills.Match(userPrompt)
	var b strings.Builder
	b.WriteString("You are a personal QQ AI agent. Answer in the user's language. ")
	b.WriteString("You may autonomously use tools because this deployment is configured for auto execution. ")
	b.WriteString("Stay within configured allowlists. Prefer concise status updates and clear final answers.\n\n")
	b.WriteString("Available local projects:\n")
	for _, p := range e.cfg.Projects {
		b.WriteString("- " + p.Name + ": " + p.Path)
		if p.RemoteHost != "" {
			b.WriteString(" on " + p.RemoteHost)
		}
		if p.Description != "" {
			b.WriteString(" - " + p.Description)
		}
		b.WriteString("\n")
	}
	if len(skills) > 0 {
		b.WriteString("\nRelevant skills:\n")
		for _, s := range skills {
			b.WriteString("## " + s.Name + "\n")
			b.WriteString(shortText(s.Content, 3000) + "\n")
		}
	}
	return b.String()
}
