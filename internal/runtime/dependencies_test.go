package runtime

import (
	"errors"
	"fmt"
	"strings"
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

func TestOnboardingExplainsIntelDependencyAndUsesNativeHomebrew(t *testing.T) {
	checks, command := checkDependencies("darwin", "arm64", func(name string) (string, error) {
		if name == "limactl" {
			return "", fmt.Errorf("%w：limactl 是 Intel 版本", errToolArchitecture)
		}
		return "/tools/" + name, nil
	})
	if command != "/opt/homebrew/bin/brew install lima" {
		t.Fatalf("suggested the wrong Homebrew: %s", command)
	}
	if checks[3].Status != "unsupported" || !strings.Contains(checks[3].Detail, "Intel") {
		t.Fatalf("architecture error hidden as an installed dependency: %+v", checks[3])
	}
}
