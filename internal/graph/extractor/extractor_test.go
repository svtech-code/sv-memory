package extractor

import (
	"testing"
)

func TestTreeSitterExtractor_Go(t *testing.T) {
	ext := NewTreeSitterExtractor()
	src := []byte(`package main
import "fmt"
type User struct { Name string }
func SayHello(name string) {
	fmt.Println("Hello " + name)
}
`)

	symbols, imports, err := ext.Extract(src, "main.go", ".go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify imports
	if len(imports) != 1 || imports[0] != "fmt" {
		t.Errorf("expected imports ['fmt'], got %v", imports)
	}

	// Verify symbols
	var foundUser, foundSayHello bool
	for _, sym := range symbols {
		if sym.Name == "User" && sym.Type == "class" {
			foundUser = true
		}
		if sym.Name == "SayHello" && sym.Type == "function" {
			foundSayHello = true
		}
	}

	if !foundUser {
		t.Error("expected class symbol 'User' not found")
	}
	if !foundSayHello {
		t.Error("expected function symbol 'SayHello' not found")
	}
}

func TestTreeSitterExtractor_Python(t *testing.T) {
	ext := NewTreeSitterExtractor()
	src := []byte(`
import os
from collections import defaultdict

class Processor:
    def process_data(self):
        pass

def run():
    p = Processor()
    p.process_data()
`)

	symbols, imports, err := ext.Extract(src, "main.py", ".py")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify imports
	expectedImports := map[string]bool{"os": true, "collections": true}
	for _, imp := range imports {
		if !expectedImports[imp] {
			t.Errorf("unexpected import %s", imp)
		}
	}

	// Verify symbols
	var foundProcessor, foundRun bool
	for _, sym := range symbols {
		if sym.Name == "Processor" && sym.Type == "class" {
			foundProcessor = true
		}
		if sym.Name == "run" && sym.Type == "function" {
			foundRun = true
		}
	}

	if !foundProcessor {
		t.Error("expected class symbol 'Processor' not found")
	}
	if !foundRun {
		t.Error("expected function symbol 'run' not found")
	}
}

func TestTreeSitterExtractor_Javascript(t *testing.T) {
	ext := NewTreeSitterExtractor()
	src := []byte(`
import React from 'react';
const fs = require('fs');

class PageComponent {
    render() {
        return null;
    }
}

function runPage() {
    console.log("running");
}
`)

	symbols, imports, err := ext.Extract(src, "main.js", ".js")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify imports
	expectedImports := map[string]bool{"react": true, "fs": true}
	for _, imp := range imports {
		if !expectedImports[imp] {
			t.Errorf("unexpected import %s", imp)
		}
	}

	// Verify symbols
	var foundPageComponent, foundRender, foundRunPage bool
	for _, sym := range symbols {
		if sym.Name == "PageComponent" && sym.Type == "class" {
			foundPageComponent = true
		}
		if sym.Name == "render" && sym.Type == "function" {
			foundRender = true
		}
		if sym.Name == "runPage" && sym.Type == "function" {
			foundRunPage = true
		}
	}

	if !foundPageComponent {
		t.Error("expected class symbol 'PageComponent' not found")
	}
	if !foundRender {
		t.Error("expected function symbol 'render' not found")
	}
	if !foundRunPage {
		t.Error("expected function symbol 'runPage' not found")
	}
}
