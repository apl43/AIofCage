package runtime

import (
	"errors"
	"testing"
)

func TestOnboardingDependenciesReportOnlyMissingTools(t *testing.T) {
	for _, test := range []struct {
		missing map[string]bool
		command string
	}{
		{map[string]bool{}, ""},
		{map[string]bool{"docker": true}, "brew install docker"},
		{map[string]bool{"colima": true, "limactl": true}, "brew install colima"},
		{map[string]bool{"colima": true, "docker": true, "limactl": true}, "brew install colima docker"},
		{map[string]bool{"limactl": true}, "brew install lima"},
	} {
		checks, command := checkDependencies("darwin", "arm64", func(name string) (string, error) {
			if test.missing[name] {
				return "", errors.New("missing")
			}
			return "/tools/" + name, nil
		})
		if command != test.command || len(checks) != 4 {
			t.Fatalf("missing %v: %s %+v", test.missing, command, checks)
		}
		for _, c := range checks {
			if (c.Status == "missing") != test.missing[c.ID] {
				t.Fatalf("wrong dependency status: %+v", c)
			}
		}
	}
	checks, _ := checkDependencies("linux", "amd64", func(string) (string, error) { return "/tool", nil })
	if checks[0].Status != "unsupported" {
		t.Fatal("unsupported platform accepted")
	}
}
