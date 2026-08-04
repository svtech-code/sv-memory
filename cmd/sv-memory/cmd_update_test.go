package main

import "testing"

func TestCompareReleaseVersions(t *testing.T) {
	tests := []struct {
		name    string
		current string
		latest  string
		want    int
	}{
		{name: "newer stable release", current: "v0.4.0", latest: "v0.5.0", want: -1},
		{name: "older stable release", current: "v0.5.0", latest: "v0.4.0", want: 1},
		{name: "equal releases", current: "v0.4.0", latest: "0.4.0", want: 0},
		{name: "stable beats prerelease", current: "v0.5.0-rc.1", latest: "v0.5.0", want: -1},
		{name: "prerelease ordering", current: "v0.5.0-rc.1", latest: "v0.5.0-rc.2", want: -1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := compareReleaseVersions(test.current, test.latest)
			if err != nil {
				t.Fatalf("compareReleaseVersions() error = %v", err)
			}
			if got != test.want {
				t.Errorf("compareReleaseVersions() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestCompareReleaseVersionsRejectsInvalidVersions(t *testing.T) {
	for _, test := range []struct {
		current string
		latest  string
	}{
		{current: "dev", latest: "v0.5.0"},
		{current: "v0.4", latest: "v0.5.0"},
		{current: "v0.4.0", latest: "latest"},
	} {
		if _, err := compareReleaseVersions(test.current, test.latest); err == nil {
			t.Errorf("compareReleaseVersions(%q, %q) expected an error", test.current, test.latest)
		}
	}
}
