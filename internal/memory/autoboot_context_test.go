package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestAutoBootBundleAndScopedSearch(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_autoboot.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-autoboot"
	err = db.RegisterProject(database, projectID, "AutoBoot Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// 1. Create a session and memory
	sess, err := StartSession(database, projectID, "Refactor database module", tempDir)
	if err != nil {
		t.Fatalf("failed to start session: %v", err)
	}
	mem := &Memory{
		ID:        "mem-boot-1",
		ProjectID: projectID,
		Category:  "architecture",
		What:      "Use SQLite WAL mode",
		Why:       "Improve concurrency",
		WherePath: "internal/db/pool.go",
		Learned:   "WAL mode avoids locks",
		SessionID: sess.ID,
		CreatedAt: time.Now(),
	}
	if _, saveErr := SaveMemory(database, mem); saveErr != nil {
		t.Fatalf("failed to save memory: %v", saveErr)
	}

	// 2. Test GetAutoBootBundle
	bundle, err := GetAutoBootBundle(context.Background(), database, projectID, AutoBootOptions{})
	if err != nil {
		t.Fatalf("failed to get autoboot bundle: %v", err)
	}
	if !strings.Contains(bundle, "Use SQLite WAL mode") {
		t.Errorf("expected autoboot bundle to contain architectural decision, got:\n%s", bundle)
	}

	// 3. Test SearchMemoriesCompactScoped with BM25 & path filter
	results, err := SearchMemoriesCompactScoped(database, projectID, "SQLite", "", "internal/db", "all", 10, 0)
	if err != nil {
		t.Fatalf("failed scoped search: %v", err)
	}
	if len(results) != 1 || results[0].ID != "mem-boot-1" {
		t.Errorf("expected 1 result with ID mem-boot-1, got %d results", len(results))
	}

	// Test path filter miss
	noResults, err := SearchMemoriesCompactScoped(database, projectID, "SQLite", "", "internal/mcp", "all", 10, 0)
	if err != nil {
		t.Fatalf("failed scoped search miss: %v", err)
	}
	if len(noResults) != 0 {
		t.Errorf("expected 0 results for non-matching path, got %d", len(noResults))
	}
}

func TestAutoBootBundleFullSessionFlow(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_autoboot_flow.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-autoboot-flow"
	err = db.RegisterProject(database, projectID, "AutoBoot Flow Proj", tempDir)
	if err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Session 1: full lifecycle with architecture + decision memories.
	sess1, err := StartSession(database, projectID, "Refactor database module", tempDir)
	if err != nil {
		t.Fatalf("failed to start session 1: %v", err)
	}

	// Session 1 memories: these appear in the session context section and must
	// NOT be repeated in the per-category sections (dedup).
	sessMems := []*Memory{
		{ID: "mem-flow-1", ProjectID: projectID, Category: "architecture", What: "Use SQLite WAL mode", Why: "Improve concurrency", WherePath: "internal/db/pool.go", Learned: "WAL avoids writer locks", SessionID: sess1.ID, CreatedAt: time.Now().Add(-4 * time.Hour)},
		{ID: "mem-flow-3", ProjectID: projectID, Category: "decision", What: "Keep JSON over NDJSON", Why: "Backwards compatibility", Learned: "JSON stays for legacy commits", SessionID: sess1.ID, CreatedAt: time.Now().Add(-3 * time.Hour)},
		{ID: "mem-flow-5", ProjectID: projectID, Category: "bugfix", What: "Fix empty FTS5 query crash", Why: "Sanitize empty tokens", SessionID: sess1.ID, CreatedAt: time.Now().Add(-2 * time.Hour)},
	}
	// Extra memories outside the session so the per-category sections have
	// content even after session IDs are deduped.
	extraMems := []*Memory{
		{ID: "mem-extra-1", ProjectID: projectID, Category: "architecture", What: "Adopt Bun as package manager", Why: "Faster installs", CreatedAt: time.Now().Add(-90 * time.Minute)},
		{ID: "mem-extra-2", ProjectID: projectID, Category: "decision", What: "Use typed constants", Why: "Avoid magic strings", CreatedAt: time.Now().Add(-80 * time.Minute)},
		{ID: "mem-extra-3", ProjectID: projectID, Category: "standard", What: "Use Conventional Commits in Spanish", Why: "Team convention", CreatedAt: time.Now().Add(-70 * time.Minute)},
		{ID: "mem-extra-4", ProjectID: projectID, Category: "bugfix", What: "Fix index out of range", Why: "Guard empty slice", CreatedAt: time.Now().Add(-60 * time.Minute)},
	}
	for _, m := range append(sessMems, extraMems...) {
		if _, saveErr := SaveMemory(database, m); saveErr != nil {
			t.Fatalf("failed to save memory %s: %v", m.ID, saveErr)
		}
	}

	err = SaveSessionSummary(database, sess1.ID, "Refactor database module", "Discovered WAL + Bun", "Wrote session flow", "Add edge case tests", "internal/memory/memory_session.go")
	if err != nil {
		t.Fatalf("failed to save session summary: %v", err)
	}
	err = EndSession(database, sess1.ID, "Completed refactor session")
	if err != nil {
		t.Fatalf("failed to end session 1: %v", err)
	}

	// Session 2: starting a new session should surface session 1 context.
	_, err = StartSession(database, projectID, "New feature task", tempDir)
	if err != nil {
		t.Fatalf("failed to start session 2: %v", err)
	}

	bundle, err := GetAutoBootBundle(context.Background(), database, projectID, AutoBootOptions{})
	if err != nil {
		t.Fatalf("failed to get autoboot bundle: %v", err)
	}

	assertions := map[string]string{
		"## Previous Session Context":             "previous session section header",
		"**Session ID:** " + sess1.ID:             "session 1 ID",
		"**Goal:** Refactor database module":      "session 1 goal",
		"**Summary:** Completed refactor session": "session 1 summary",
		"**Memories saved (3):**":                 "session 1 memory list",
		"**Key Architectural Decisions:**":        "architectural decisions section",
		"Adopt Bun as package manager":            "architecture memory title",
		"Use typed constants":                     "decision memory title",
		"*Why:* Faster installs":                  "architecture why rationale",
		"**Standards & Conventions:**":            "standards section",
		"Use Conventional Commits in Spanish":     "standard memory title",
		"**Recent Work & Known Issues:**":         "recent work section",
		"Fix index out of range":                  "bugfix memory title",
		"Use SQLite WAL mode":                     "session architecture memory listed in session context",
	}
	for want, desc := range assertions {
		if !strings.Contains(bundle, want) {
			t.Errorf("autoboot bundle missing %s (expected substring %q), got:\n%s", desc, want, bundle)
		}
	}

	// Dedup: a memory shown in the previous-session section must not repeat
	// its rationale under Key Architectural Decisions.
	if strings.Contains(bundle, "*Why:* Improve concurrency") {
		t.Errorf("expected session memory deduped from decisions section, got:\n%s", bundle)
	}
}

func TestAutoBootBundleGoalAware(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "bundle_goal.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	defer database.Close()

	const projectID = "proj-bundle-goal"
	if err = db.RegisterProject(database, projectID, "Bundle Goal", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	// A completed session so the previous-session section is produced; its
	// memory is unrelated and deduped, leaving the decisions section free.
	sess, err := StartSession(database, projectID, "Session one", tempDir)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	if _, err = SaveMemory(database, &Memory{
		ProjectID: projectID, Category: "architecture", What: "Session only memory",
		Why: "unrelated", SessionID: sess.ID, CreatedAt: time.Now().Add(-3 * time.Hour),
	}); err != nil {
		t.Fatalf("save session memory: %v", err)
	}
	if err = SaveSessionSummary(database, sess.ID, "Session one", "d", "a", "n", "f"); err != nil {
		t.Fatalf("SaveSessionSummary: %v", err)
	}
	if err = EndSession(database, sess.ID, "done"); err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	if _, err = StartSession(database, projectID, "Current task", tempDir); err != nil {
		t.Fatalf("StartSession 2: %v", err)
	}

	// Two decisions outside the session: A is older but matches the goal;
	// B is newer but unrelated.
	a, err := SaveMemory(database, &Memory{
		ProjectID: projectID, Category: "decision", What: "Graph query token efficiency",
		Why: "Reduce context tokens", CreatedAt: time.Now().Add(-90 * time.Minute),
	})
	if err != nil {
		t.Fatalf("save A: %v", err)
	}
	b, err := SaveMemory(database, &Memory{
		ProjectID: projectID, Category: "decision", What: "Agent hooks setup",
		Why: "Lifecycle wiring", CreatedAt: time.Now().Add(-80 * time.Minute),
	})
	if err != nil {
		t.Fatalf("save B: %v", err)
	}

	// Without a goal, recency wins (newer B before older A).
	rec, err := GetAutoBootBundle(context.Background(), database, projectID, AutoBootOptions{})
	if err != nil {
		t.Fatalf("GetAutoBootBundle: %v", err)
	}
	if !strings.Contains(rec, b.What) || !strings.Contains(rec, a.What) {
		t.Fatalf("expected both decisions in bundle, got:\n%s", rec)
	}
	if strings.Index(rec, b.What) >= strings.Index(rec, a.What) {
		t.Errorf("expected newer memory B before older A without a goal:\n%s", rec)
	}

	// With a goal matching A, keyword ranking puts A before B.
	g, err := GetAutoBootBundle(context.Background(), database, projectID, AutoBootOptions{Goal: "graph"})
	if err != nil {
		t.Fatalf("GetAutoBootBundle goal: %v", err)
	}
	if strings.Index(g, a.What) >= strings.Index(g, b.What) {
		t.Errorf("expected goal-relevant A before B with goal 'graph':\n%s", g)
	}
}

func TestAutoBootBundleSemanticGoal(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "bundle_goal_sem.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	defer database.Close()

	const projectID = "proj-bundle-goal-sem"
	if err = db.RegisterProject(database, projectID, "Bundle Goal Sem", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	sess, err := StartSession(database, projectID, "Session one", tempDir)
	if err != nil {
		t.Fatalf("StartSession error: %v", err)
	}
	if _, err = SaveMemory(database, &Memory{
		ProjectID: projectID, Category: "architecture", What: "Session only memory",
		Why: "unrelated", SessionID: sess.ID, CreatedAt: time.Now().Add(-3 * time.Hour),
	}); err != nil {
		t.Fatalf("save session memory: %v", err)
	}
	if err = SaveSessionSummary(database, sess.ID, "Session one", "d", "a", "n", "f"); err != nil {
		t.Fatalf("SaveSessionSummary: %v", err)
	}
	if err = EndSession(database, sess.ID, "done"); err != nil {
		t.Fatalf("EndSession: %v", err)
	}
	if _, err = StartSession(database, projectID, "Current task", tempDir); err != nil {
		t.Fatalf("StartSession 2: %v", err)
	}

	a, err := SaveMemory(database, &Memory{
		ProjectID: projectID, Category: "decision", What: "Graph query token efficiency",
		Why: "Reduce context tokens", CreatedAt: time.Now().Add(-90 * time.Minute),
	})
	if err != nil {
		t.Fatalf("save A: %v", err)
	}
	b, err := SaveMemory(database, &Memory{
		ProjectID: projectID, Category: "decision", What: "Agent hooks setup",
		Why: "Lifecycle wiring", CreatedAt: time.Now().Add(-80 * time.Minute),
	})
	if err != nil {
		t.Fatalf("save B: %v", err)
	}

	original := SemanticRunAgent
	defer func() { SemanticRunAgent = original }()

	// Semantic ranking keeps only A and appends the relevance reason.
	SemanticRunAgent = func(ctx context.Context, agent, prompt string) (string, error) {
		return `[{"id":"` + a.ID + `","relevant":true,"reason":"graph focused"}]`, nil
	}
	gs, err := GetAutoBootBundle(context.Background(), database, projectID, AutoBootOptions{Goal: "graph", Semantic: true})
	if err != nil {
		t.Fatalf("GetAutoBootBundle semantic: %v", err)
	}
	if !strings.Contains(gs, a.What) {
		t.Errorf("expected semantically relevant A in bundle, got:\n%s", gs)
	}
	if !strings.Contains(gs, "graph focused") {
		t.Errorf("expected relevance reason appended, got:\n%s", gs)
	}
	if strings.Contains(gs, b.What) {
		t.Errorf("expected unrelated B filtered out by semantic ranking, got:\n%s", gs)
	}

	// Fail-open: agent error falls back to the deterministic keyword ranking.
	SemanticRunAgent = func(ctx context.Context, agent, prompt string) (string, error) {
		return "", context.DeadlineExceeded
	}
	gf, err := GetAutoBootBundle(context.Background(), database, projectID, AutoBootOptions{Goal: "graph", Semantic: true})
	if err != nil {
		t.Fatalf("GetAutoBootBundle semantic fail-open: %v", err)
	}
	if !strings.Contains(gf, a.What) || !strings.Contains(gf, b.What) {
		t.Errorf("expected both decisions on fail-open, got:\n%s", gf)
	}
	if strings.Index(gf, a.What) >= strings.Index(gf, b.What) {
		t.Errorf("expected keyword ranking A before B on fail-open:\n%s", gf)
	}
}

func TestPerSectionCapsPinnedFirst(t *testing.T) {
	sections := []bundleSection{{title: "dec", where: "x", withWhy: false, cap: 2}}
	bySection := [][]bundleCandidate{
		{
			{id: "a", what: "Graph query", why: "tokens", createdAt: time.Now().Add(-10 * time.Minute)},
			{id: "b", what: "hooks", why: "wiring", pinned: true, createdAt: time.Now().Add(-5 * time.Minute)},
			{id: "c", what: "memory", why: "sync", createdAt: time.Now()},
		},
	}
	sel := perSectionCaps(bySection, sections, true, "graph")
	var got []string
	for _, c := range sel[0] {
		got = append(got, c.id)
	}
	want := []string{"b", "a"}
	if len(got) != len(want) {
		t.Fatalf("perSectionCaps = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("perSectionCaps order = %v, want %v", got, want)
		}
	}
}

func TestAutoBootBundleSurfacesPendingConflicts(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "bundle_conflict.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	defer database.Close()

	const projectID = "proj-bundle-conflict"
	if err = db.RegisterProject(database, projectID, "Bundle Conflict", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	// With no conflicts the bundle carries no conflict line.
	clean, err := GetAutoBootBundle(context.Background(), database, projectID, AutoBootOptions{})
	if err != nil {
		t.Fatalf("GetAutoBootBundle clean: %v", err)
	}
	if strings.Contains(clean, "Pending memory conflicts") {
		t.Errorf("expected no conflict line when none exist, got:\n%s", clean)
	}

	// Two memories plus a pending conflicts_with relation created by a real
	// conflict scan (ScanConflicts marks candidates status='pending', unlike
	// SaveJudgment which records an explicit judgment as 'judged').
	if _, err = SaveMemory(database, &Memory{ProjectID: projectID, Category: "decision", What: "Use PostgreSQL for users data storage", Why: "w", Learned: "l"}); err != nil {
		t.Fatalf("save a: %v", err)
	}
	if _, err = SaveMemory(database, &Memory{ProjectID: projectID, Category: "decision", What: "Replace PostgreSQL users storage with MongoDB", Why: "w", Learned: "l"}); err != nil {
		t.Fatalf("save b: %v", err)
	}
	if _, err = ScanConflicts(database, projectID, true, 5, 0.45); err != nil {
		t.Fatalf("ScanConflicts: %v", err)
	}

	bundle, err := GetAutoBootBundle(context.Background(), database, projectID, AutoBootOptions{})
	if err != nil {
		t.Fatalf("GetAutoBootBundle conflict: %v", err)
	}
	if !strings.Contains(bundle, "Pending memory conflicts") {
		t.Errorf("expected pending conflict line in bundle, got:\n%s", bundle)
	}
}

func TestAutoBootBundleSurfacesActiveChanges(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "bundle_changes.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	defer database.Close()

	const projectID = "proj-bundle-changes"
	if err = db.RegisterProject(database, projectID, "Bundle Changes", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	// Without changes, bundle should not mention active changes.
	clean, err := GetAutoBootBundle(context.Background(), database, projectID, AutoBootOptions{})
	if err != nil {
		t.Fatalf("GetAutoBootBundle clean: %v", err)
	}
	if strings.Contains(clean, "Active changes") {
		t.Errorf("expected no active changes when none exist, got:\n%s", clean)
	}

	// Create a change with task progress.
	c, err := CreateChange(database, projectID, "add-auth", "Add Authentication", "Implement login flow", "", "internal/auth/", "",
		"- [x] 1.1 Setup context\n- [ ] 1.2 Create login form\n- [ ] 1.3 Add session storage")
	if err != nil {
		t.Fatalf("CreateChange: %v", err)
	}
	if _, err = UpdateChangeStatus(database, projectID, c.ID, ChangeStatusProposed); err != nil {
		t.Fatalf("UpdateChangeStatus: %v", err)
	}

	bundle, err := GetAutoBootBundle(context.Background(), database, projectID, AutoBootOptions{})
	if err != nil {
		t.Fatalf("GetAutoBootBundle with changes: %v", err)
	}
	if !strings.Contains(bundle, "Active changes") {
		t.Errorf("expected 'Active changes' section, got:\n%s", bundle)
	}
	if !strings.Contains(bundle, "add-auth") {
		t.Errorf("expected change slug in bundle, got:\n%s", bundle)
	}
	if !strings.Contains(bundle, "1/3") || !strings.Contains(bundle, "33%") {
		t.Errorf("expected task progress 1/3 (33%%), got:\n%s", bundle)
	}
}

func TestAutoBootBundleSurfacesPostmortemAndQA(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "bundle_pm_qa.db"))
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}
	defer database.Close()

	projectID := "proj-bundle-pm-qa"
	if regErr := db.RegisterProject(database, projectID, "Bundle PM QA", tempDir); regErr != nil {
		t.Fatalf("failed to register project: %v", regErr)
	}

	// Seed a postmortem and a qa memory. No session exists, so only the
	// per-category bundle sections are produced.
	if _, err = SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "postmortem",
		What:      "Push failed on CI Lint",
		Why:       "govet shadow not caught locally",
		Learned:   "Run golangci-lint before pushing",
	}); err != nil {
		t.Fatalf("failed to save postmortem: %v", err)
	}
	if _, err = SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "qa",
		What:      "Why does sv_mem_session_start need token_budget",
		Why:       "It returns the largest pre-tool payload",
		Learned:   "So the bundle is bounded by max_response_tokens",
	}); err != nil {
		t.Fatalf("failed to save qa: %v", err)
	}

	bundle, err := GetAutoBootBundle(context.Background(), database, projectID, AutoBootOptions{})
	if err != nil {
		t.Fatalf("failed to get autoboot bundle: %v", err)
	}

	// The postmortem section surfaces with its rationale (withWhy=true)...
	if !strings.Contains(bundle, "**Postmortems & Lessons Learned:**") {
		t.Errorf("expected postmortem section in bundle, got:\n%s", bundle)
	}
	if !strings.Contains(bundle, "Push failed on CI Lint") {
		t.Errorf("expected postmortem memory surfaced, got:\n%s", bundle)
	}
	if !strings.Contains(bundle, "*Why:* govet shadow not caught locally") {
		t.Errorf("expected postmortem rationale shown (withWhy), got:\n%s", bundle)
	}

	// ...and the qa section surfaces without its rationale (withWhy=false) so
	// the bundle stays compact.
	if !strings.Contains(bundle, "**Recent Q&A:**") {
		t.Errorf("expected qa section in bundle, got:\n%s", bundle)
	}
	if !strings.Contains(bundle, "Why does sv_mem_session_start need token_budget") {
		t.Errorf("expected qa memory surfaced, got:\n%s", bundle)
	}
	if strings.Contains(bundle, "*Why:* It returns the largest pre-tool payload") {
		t.Errorf("expected qa rationale omitted (withWhy=false), got:\n%s", bundle)
	}
}

func TestAutoBootBundleSurfacesIdeasAndFiltersPostCommit(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "bundle_ideas.db"))
	if err != nil {
		t.Fatalf("InitDB error: %v", err)
	}
	defer database.Close()

	const projectID = "proj-bundle-ideas"
	if err = db.RegisterProject(database, projectID, "Bundle Ideas", tempDir); err != nil {
		t.Fatalf("RegisterProject error: %v", err)
	}

	// 1. Save an unresolved idea and a resolved idea.
	if _, err = SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "idea",
		What:      "Add dark mode toggle to TUI",
		Why:       "Requested by user during review",
		Learned:   "Track UI ideas across sessions",
	}); err != nil {
		t.Fatalf("SaveMemory idea: %v", err)
	}
	if _, err = SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "idea",
		What:      "RESUELTO: Old completed idea",
		Why:       "Already done",
		Learned:   "Resolved ideas are archived",
	}); err != nil {
		t.Fatalf("SaveMemory resolved idea: %v", err)
	}

	// 2. Save a real journal and a noisy post-commit journal.
	if _, err = SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "journal",
		What:      "Completed Phase 1 graph optimization",
		Why:       "Reduced query latency",
		Learned:   "Use indexed lookups",
	}); err != nil {
		t.Fatalf("SaveMemory real journal: %v", err)
	}
	if _, err = SaveMemory(database, &Memory{
		ProjectID: projectID,
		Category:  "journal",
		What:      "git commit: fix(lint): whitespace",
		Why:       "automatic post-commit capture for 999999",
		Learned:   "Committed 1 file",
	}); err != nil {
		t.Fatalf("SaveMemory post-commit journal: %v", err)
	}

	// 3. Create an applied change and a proposed change.
	cApplied, err := CreateChange(database, projectID, "old-applied-spec", "Old Applied Spec", "what", "", "", "", "")
	if err != nil {
		t.Fatalf("CreateChange applied: %v", err)
	}
	if _, err = UpdateChangeStatus(database, projectID, cApplied.ID, ChangeStatusApplied); err != nil {
		t.Fatalf("UpdateChangeStatus applied: %v", err)
	}
	cProp, err := CreateChange(database, projectID, "pending-spec", "Pending Spec", "what", "", "", "", "- [ ] Task 1")
	if err != nil {
		t.Fatalf("CreateChange proposed: %v", err)
	}
	if _, err = UpdateChangeStatus(database, projectID, cProp.ID, ChangeStatusProposed); err != nil {
		t.Fatalf("UpdateChangeStatus proposed: %v", err)
	}

	bundle, err := GetAutoBootBundle(context.Background(), database, projectID, AutoBootOptions{})
	if err != nil {
		t.Fatalf("GetAutoBootBundle: %v", err)
	}

	if !strings.Contains(bundle, "**Active Ideas:**") || !strings.Contains(bundle, "Add dark mode toggle to TUI") {
		t.Errorf("expected unresolved idea in Auto-Boot bundle, got:\n%s", bundle)
	}
	if strings.Contains(bundle, "RESUELTO: Old completed idea") {
		t.Errorf("expected resolved idea to be excluded from Auto-Boot bundle, got:\n%s", bundle)
	}
	if !strings.Contains(bundle, "Completed Phase 1 graph optimization") {
		t.Errorf("expected real journal in Auto-Boot bundle, got:\n%s", bundle)
	}
	if strings.Contains(bundle, "git commit: fix(lint): whitespace") {
		t.Errorf("expected post-commit journal excluded from Auto-Boot bundle, got:\n%s", bundle)
	}
	if !strings.Contains(bundle, "pending-spec") {
		t.Errorf("expected pending spec in Active changes, got:\n%s", bundle)
	}
	if strings.Contains(bundle, "old-applied-spec") {
		t.Errorf("expected applied spec slug to be summarized rather than itemized, got:\n%s", bundle)
	}
	if !strings.Contains(bundle, "+1 applied changes") {
		t.Errorf("expected applied changes summary count, got:\n%s", bundle)
	}
}
