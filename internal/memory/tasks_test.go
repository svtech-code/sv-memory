package memory

import (
	"testing"
)

func TestParseTaskProgress(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantTotal int
		wantComp  int
		wantPend  int
		wantPct   int
		wantSumm  string
	}{
		{
			name:      "empty tasks",
			input:     "",
			wantTotal: 0,
			wantComp:  0,
			wantPend:  0,
			wantPct:   0,
			wantSumm:  "",
		},
		{
			name: "plain prose without checkboxes",
			input: `
Here is a list of things:
1. First item
2. Second item
`,
			wantTotal: 0,
			wantComp:  0,
			wantPend:  0,
			wantPct:   0,
			wantSumm:  "",
		},
		{
			name: "mixed checklist standard markdown",
			input: `
- [ ] Task 1: Initialize module
- [x] Task 2: Implement parser
- [ ] Task 3: Add unit tests
- [X] Task 4: Validate CI
`,
			wantTotal: 4,
			wantComp:  2,
			wantPend:  2,
			wantPct:   50,
			wantSumm:  "2/4 (50%)",
		},
		{
			name: "asterisk bullets and mixed casing",
			input: `
* [x] Task A
* [ ] Task B
* [x] Task C
`,
			wantTotal: 3,
			wantComp:  2,
			wantPend:  1,
			wantPct:   66,
			wantSumm:  "2/3 (66%)",
		},
		{
			name: "all completed",
			input: `
- [x] Task 1
- [x] Task 2
`,
			wantTotal: 2,
			wantComp:  2,
			wantPend:  0,
			wantPct:   100,
			wantSumm:  "2/2 (100%)",
		},
		{
			name: "all pending",
			input: `
- [ ] Task 1
- [ ] Task 2
- [ ] Task 3
`,
			wantTotal: 3,
			wantComp:  0,
			wantPend:  3,
			wantPct:   0,
			wantSumm:  "0/3 (0%)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTaskProgress(tt.input)
			if got.Total != tt.wantTotal ||
				got.Completed != tt.wantComp ||
				got.Pending != tt.wantPend ||
				got.Percent != tt.wantPct ||
				got.Summary != tt.wantSumm {
				t.Errorf("ParseTaskProgress() = %+v, want Total=%d Comp=%d Pend=%d Pct=%d Summ=%q",
					got, tt.wantTotal, tt.wantComp, tt.wantPend, tt.wantPct, tt.wantSumm)
			}
		})
	}
}
