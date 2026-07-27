package hook

import (
	"os"
	"testing"
)

func BenchmarkHooksInstallAll(b *testing.B) {
	for i := 0; i < b.N; i++ {
		tempDir, err := os.MkdirTemp("", "sv-hook-bench-all")
		if err != nil {
			b.Fatalf("failed to create temp dir: %v", err)
		}

		eng := New(tempDir, ModeSoft)
		results := eng.Install(nil)

		for _, r := range results {
			if r.Err != nil {
				b.Fatalf("install %s failed: %v", r.Platform, r.Err)
			}
		}

		os.RemoveAll(tempDir)
	}
}

func BenchmarkHooksInstallByPlatform(b *testing.B) {
	platforms := []Platform{PlatformClaudeCode, PlatformCodex, PlatformAntigravity, PlatformOpenCode}
	for _, p := range platforms {
		b.Run(string(p), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				tempDir, err := os.MkdirTemp("", "sv-hook-bench-"+string(p))
				if err != nil {
					b.Fatalf("failed to create temp dir: %v", err)
				}

				eng := New(tempDir, ModeSoft)
				results := eng.Install([]Platform{p})

				if results[0].Err != nil {
					b.Fatalf("install %s failed: %v", p, results[0].Err)
				}

				os.RemoveAll(tempDir)
			}
		})
	}
}

func BenchmarkHooksUninstallAll(b *testing.B) {
	for i := 0; i < b.N; i++ {
		tempDir, err := os.MkdirTemp("", "sv-hook-bench-uninstall")
		if err != nil {
			b.Fatalf("failed to create temp dir: %v", err)
		}

		eng := New(tempDir, ModeSoft)
		eng.Install(nil)
		results := eng.Uninstall(nil)

		for _, r := range results {
			if r.Err != nil {
				b.Fatalf("uninstall %s failed: %v", r.Platform, r.Err)
			}
		}

		os.RemoveAll(tempDir)
	}
}
