package graph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/svtech-code/sv-memory/internal/db"
)

func TestCalculateBlastRadius(t *testing.T) {
	tempDir := t.TempDir()
	database, err := db.InitDB(filepath.Join(tempDir, "blast_test.db"))
	if err != nil {
		t.Fatalf("failed to init DB: %v", err)
	}
	defer database.Close()

	projectID := "proj-blast-test"
	if err = db.RegisterProject(database, projectID, "Blast Proj", tempDir); err != nil {
		t.Fatalf("failed to register project: %v", err)
	}

	// Create 3 files:
	// core.go: func Core()
	// service.go: func Service() { Core() } (hop 1)
	// controller.go: func Controller() { Service() } (hop 2)
	coreSrc := "package main\nfunc Core() {}\n"
	serviceSrc := "package main\nfunc Service() { Core() }\n"
	controllerSrc := "package main\nfunc Controller() { Service() }\n"

	if err = os.WriteFile(filepath.Join(tempDir, "core.go"), []byte(coreSrc), 0644); err != nil {
		t.Fatalf("failed writing core.go: %v", err)
	}
	if err = os.WriteFile(filepath.Join(tempDir, "service.go"), []byte(serviceSrc), 0644); err != nil {
		t.Fatalf("failed writing service.go: %v", err)
	}
	if err = os.WriteFile(filepath.Join(tempDir, "controller.go"), []byte(controllerSrc), 0644); err != nil {
		t.Fatalf("failed writing controller.go: %v", err)
	}

	if err = SyncGraph(database, projectID, tempDir); err != nil {
		t.Fatalf("SyncGraph failed: %v", err)
	}

	// Query blast radius for core.go:Core
	blast, err := CalculateBlastRadius(database, projectID, "core.go:Core", 3, 10)
	if err != nil {
		t.Fatalf("CalculateBlastRadius failed: %v", err)
	}

	if len(blast) < 2 {
		t.Fatalf("expected at least 2 blast radius nodes, got %d: %+v", len(blast), blast)
	}

	// Verify hop 1 is service.go:Service and hop 2 is controller.go:Controller
	var foundService, foundController bool
	for _, b := range blast {
		if b.ID == "service.go:Service" {
			foundService = true
			if b.Depth != 1 {
				t.Errorf("expected Service depth 1, got %d", b.Depth)
			}
		}
		if b.ID == "controller.go:Controller" {
			foundController = true
			if b.Depth != 2 {
				t.Errorf("expected Controller depth 2, got %d", b.Depth)
			}
		}
	}

	if !foundService {
		t.Errorf("expected to find service.go:Service in blast radius, got %+v", blast)
	}
	if !foundController {
		t.Errorf("expected to find controller.go:Controller in blast radius, got %+v", blast)
	}
}
