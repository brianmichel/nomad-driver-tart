package driver

import (
	"strings"
	"testing"

	"github.com/hashicorp/nomad/plugins/drivers"
)

func TestTartEnvListIncludesSoftnetPath(t *testing.T) {
	t.Setenv("PATH", "/custom/bin")
	env := tartEnvList(&drivers.TaskConfig{})

	var pathValue string
	for _, entry := range env {
		if strings.HasPrefix(entry, "PATH=") {
			pathValue = strings.TrimPrefix(entry, "PATH=")
			break
		}
	}

	if pathValue == "" {
		t.Fatal("expected PATH in environment")
	}
	if !strings.Contains(pathValue, "/opt/zerobrew/bin") {
		t.Fatalf("expected PATH to include /opt/zerobrew/bin, got %q", pathValue)
	}
	if !strings.Contains(pathValue, "/custom/bin") {
		t.Fatalf("expected PATH to include inherited host PATH, got %q", pathValue)
	}
}

func TestTartEnvListPreservesTaskPath(t *testing.T) {
	tc := &drivers.TaskConfig{Env: map[string]string{"PATH": "/task/bin"}}
	env := tartEnvList(tc)

	var pathValue string
	for _, entry := range env {
		if strings.HasPrefix(entry, "PATH=") {
			pathValue = strings.TrimPrefix(entry, "PATH=")
			break
		}
	}

	if !strings.Contains(pathValue, "/task/bin") {
		t.Fatalf("expected PATH to preserve task PATH, got %q", pathValue)
	}
	if !strings.Contains(pathValue, "/opt/zerobrew/bin") {
		t.Fatalf("expected PATH to include softnet path, got %q", pathValue)
	}
}

func TestTartEnvListDoesNotDependOnAmbientPath(t *testing.T) {
	t.Setenv("PATH", "")
	env := tartEnvList(&drivers.TaskConfig{})

	var found bool
	for _, entry := range env {
		if entry == "PATH="+tartDefaultPath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected default PATH fallback, got %v", env)
	}
}
