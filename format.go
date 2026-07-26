package main

import (
	"regexp"
	"strings"
)

func formatQQText(text string, cfg *Config) string {
	if cfg == nil || !cfg.Response.PlainText {
		return strings.TrimSpace(text)
	}
	return plainTextMarkdown(text)
}

func plainTextMarkdown(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = regexp.MustCompile("(?s)```[a-zA-Z0-9_-]*\\n(.*?)```").ReplaceAllString(text, "$1")
	text = regexp.MustCompile("`([^`]+)`").ReplaceAllString(text, "$1")
	text = regexp.MustCompile("\\*\\*([^*]+)\\*\\*").ReplaceAllString(text, "$1")
	text = regexp.MustCompile("__([^_]+)__").ReplaceAllString(text, "$1")
	text = regexp.MustCompile("(?m)^#{1,6}\\s*").ReplaceAllString(text, "")
	text = regexp.MustCompile("(?m)^>\\s?").ReplaceAllString(text, "")
	text = regexp.MustCompile("\\[([^\\]]+)\\]\\(([^)]+)\\)").ReplaceAllString(text, "$1 ($2)")
	return strings.TrimSpace(text)
}
