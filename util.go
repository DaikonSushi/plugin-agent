package main

import (
	"fmt"
	"strings"
)

func formatInt(v int64) string {
	return fmt.Sprintf("%d", v)
}

func shortText(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
