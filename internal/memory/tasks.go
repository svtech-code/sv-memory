package memory

import (
	"fmt"
	"strings"
)

// TaskProgress represents the execution state of checklist tasks inside a
// change or markdown block.
type TaskProgress struct {
	Total     int    `json:"total"`
	Completed int    `json:"completed"`
	Pending   int    `json:"pending"`
	Percent   int    `json:"percent"`
	Summary   string `json:"summary"` // e.g. "2/4 (50%)" or "" if total == 0
}

// ParseTaskProgress analyzes markdown task checklists (- [ ] / - [x]) and
// calculates progress metrics. It is token-efficient and lightweight.
func ParseTaskProgress(tasks string) TaskProgress {
	lines := strings.Split(tasks, "\n")
	total := 0
	completed := 0

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if isPendingTask(trimmed) {
			total++
		} else if isCompletedTask(trimmed) {
			total++
			completed++
		}
	}

	if total == 0 {
		return TaskProgress{}
	}

	pending := total - completed
	percent := (completed * 100) / total

	return TaskProgress{
		Total:     total,
		Completed: completed,
		Pending:   pending,
		Percent:   percent,
		Summary:   fmt.Sprintf("%d/%d (%d%%)", completed, total, percent),
	}
}

func isPendingTask(s string) bool {
	return strings.HasPrefix(s, "- [ ]") ||
		strings.HasPrefix(s, "* [ ]")
}

func isCompletedTask(s string) bool {
	return strings.HasPrefix(s, "- [x]") ||
		strings.HasPrefix(s, "- [X]") ||
		strings.HasPrefix(s, "* [x]") ||
		strings.HasPrefix(s, "* [X]")
}
