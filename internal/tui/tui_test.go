package tui

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/svtech-code/sv-memory/internal/memory"
)

func TestRenderRecent(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		out := renderRecent(nil, errors.New("boom"))
		if !strings.Contains(out, "boom") {
			t.Errorf("expected error surfaced, got %q", out)
		}
	})

	t.Run("empty", func(t *testing.T) {
		out := renderRecent(nil, nil)
		if !strings.Contains(out, "No memories") {
			t.Errorf("expected empty message, got %q", out)
		}
	})

	t.Run("results", func(t *testing.T) {
		mems := []*memory.MemorySearchResult{
			{ID: "m1", Category: "decision", What: "Use Postgres", CreatedAt: time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)},
			{ID: "m2", Category: "bugfix", What: "Fix cache", CreatedAt: time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC)},
		}
		out := renderRecent(mems, nil)
		if !strings.Contains(out, "2 recent memories") {
			t.Errorf("expected count header, got %q", out)
		}
		if !strings.Contains(out, "DECISION") || !strings.Contains(out, "m1") {
			t.Errorf("expected first memory rendered, got %q", out)
		}
		if !strings.Contains(out, "BUGFIX") || !strings.Contains(out, "m2") {
			t.Errorf("expected second memory rendered, got %q", out)
		}
		if !strings.Contains(out, "2026-08-01") {
			t.Errorf("expected date rendered, got %q", out)
		}
	})
}

func TestRenderSearchResults(t *testing.T) {
	t.Run("error", func(t *testing.T) {
		out := renderSearchResults(nil, errors.New("search failed"))
		if !strings.Contains(out, "search failed") {
			t.Errorf("expected error surfaced, got %q", out)
		}
	})

	t.Run("empty", func(t *testing.T) {
		out := renderSearchResults(nil, nil)
		if !strings.Contains(out, "No matching") {
			t.Errorf("expected empty message, got %q", out)
		}
	})

	t.Run("results", func(t *testing.T) {
		mems := []*memory.MemorySearchResult{
			{ID: "a1", Category: "qa", What: "How does sync work"},
		}
		out := renderSearchResults(mems, nil)
		if !strings.Contains(out, "1 matching") {
			t.Errorf("expected count header, got %q", out)
		}
		if !strings.Contains(out, "QA") || !strings.Contains(out, "a1") || !strings.Contains(out, "sync work") {
			t.Errorf("expected memory rendered, got %q", out)
		}
	})
}

func TestRenderMemoryDetail(t *testing.T) {
	t.Run("nil memory", func(t *testing.T) {
		out := renderMemoryDetail(nil, nil)
		if !strings.Contains(out, "not found") {
			t.Errorf("expected not found message, got %q", out)
		}
	})

	t.Run("with error", func(t *testing.T) {
		out := renderMemoryDetail(nil, errors.New("db closed"))
		if !strings.Contains(out, "db closed") {
			t.Errorf("expected error surfaced, got %q", out)
		}
	})

	t.Run("full detail", func(t *testing.T) {
		mem := &memory.Memory{
			ID:        "m-1",
			Category:  "architecture",
			What:      "Design decision",
			Why:       "Because reasons",
			Learned:   "Lesson learned",
			TopicKey:  "decision/design",
			WherePath: "internal/graph",
			CreatedAt: time.Date(2026, 7, 15, 12, 30, 0, 0, time.UTC),
		}
		out := renderMemoryDetail(mem, nil)
		for _, want := range []string{"Design decision", "ARCHITECTURE", "m-1", "decision/design", "Because reasons", "Lesson learned", "internal/graph"} {
			if !strings.Contains(out, want) {
				t.Errorf("expected %q in detail output, got %q", want, out)
			}
		}
	})
}

func TestShowBannerTUI(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	showBannerTUI()
	_ = w.Close()
	os.Stdout = oldStdout

	out, _ := io.ReadAll(r)
	text := string(out)
	for _, want := range []string{"Context Memory", "Code Graph Builder", "Prevent context amnesia", "╔", "╗"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected banner to contain %q", want)
		}
	}
}

func TestTUITheme(t *testing.T) {
	theme := Theme()
	if theme == nil {
		t.Fatal("Theme() returned nil")
	}
	// The theme must be fully wired: every focused element style should be
	// non-zero, otherwise the TUI would render without its brand styling.
	if theme.Focused.Base.GetBorderStyle() == (lipgloss.Border{}) {
		t.Error("expected focused base to have a border style")
	}
	if theme.Focused.Title.GetForeground() == nil {
		t.Error("expected focused title to have a foreground color")
	}
	if theme.Focused.TextInput.Prompt.GetForeground() == nil {
		t.Error("expected focused text input prompt to have a foreground color")
	}
}
