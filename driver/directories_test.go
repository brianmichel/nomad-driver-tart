package driver

import (
	"slices"
	"testing"

	"github.com/hashicorp/nomad/plugins/drivers"
)

func TestBuildDirectoryArgs_None(t *testing.T) {
	got, err := buildDirectoryArgs(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no args, got %v", got)
	}
}

func TestBuildDirectoryArgs_SimplePath(t *testing.T) {
	dirs := []DirectoryMount{{Path: "/host/data"}}
	got, err := buildDirectoryArgs(dirs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--dir=/host/data"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildDirectoryArgs_ReadOnly(t *testing.T) {
	dirs := []DirectoryMount{{Path: "/host/secrets", Options: &DirectoryOptions{ReadOnly: true}}}
	got, err := buildDirectoryArgs(dirs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--dir=/host/secrets:ro"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildDirectoryArgs_Tag(t *testing.T) {
	dirs := []DirectoryMount{{Path: "/host/assets", Options: &DirectoryOptions{Tag: "assets"}}}
	got, err := buildDirectoryArgs(dirs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--dir=/host/assets:tag=assets"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildDirectoryArgs_ReadOnlyAndTag(t *testing.T) {
	dirs := []DirectoryMount{{Path: "/host/shared", Options: &DirectoryOptions{ReadOnly: true, Tag: "shared"}}}
	got, err := buildDirectoryArgs(dirs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--dir=/host/shared:ro,tag=shared"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildDirectoryArgs_RequiresPath(t *testing.T) {
	dirs := []DirectoryMount{{}}
	if _, err := buildDirectoryArgs(dirs); err == nil {
		t.Fatalf("expected error for empty path, got nil")
	}
}

func TestResolveDirectoryMounts_NomadTaskPaths(t *testing.T) {
	cfg := &drivers.TaskConfig{
		AllocDir: "/opt/nomad/alloc/1234",
		Name:     "vm",
	}

	dirs := []DirectoryMount{
		{Name: "local", Path: "${NOMAD_TASK_DIR}"},
		{Name: "alloc", Path: "${NOMAD_ALLOC_DIR}"},
		{Name: "secrets", Path: "${NOMAD_SECRETS_DIR}"},
		{Name: "nested", Path: "${NOMAD_TASK_DIR}/downloads"},
	}

	got := resolveDirectoryMounts(cfg, dirs)
	want := []DirectoryMount{
		{Name: "local", Path: "/opt/nomad/alloc/1234/vm/local"},
		{Name: "alloc", Path: "/opt/nomad/alloc/1234/alloc"},
		{Name: "secrets", Path: "/opt/nomad/alloc/1234/vm/secrets"},
		{Name: "nested", Path: "/opt/nomad/alloc/1234/vm/local/downloads"},
	}

	if !slices.Equal(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	if dirs[0].Path != "${NOMAD_TASK_DIR}" {
		t.Fatalf("expected input slice to remain unchanged, got %+v", dirs)
	}
}

func TestResolveDirectoryMounts_ImageIsolationPaths(t *testing.T) {
	cfg := &drivers.TaskConfig{
		AllocDir: "/opt/nomad/alloc/5678",
		Name:     "vm",
	}

	dirs := []DirectoryMount{
		{Name: "local", Path: "/local"},
		{Name: "alloc", Path: "/alloc"},
		{Name: "secrets", Path: "/secrets"},
		{Name: "nested", Path: "/local/downloads"},
	}

	got := resolveDirectoryMounts(cfg, dirs)
	want := []DirectoryMount{
		{Name: "local", Path: "/opt/nomad/alloc/5678/vm/local"},
		{Name: "alloc", Path: "/opt/nomad/alloc/5678/alloc"},
		{Name: "secrets", Path: "/opt/nomad/alloc/5678/vm/secrets"},
		{Name: "nested", Path: "/opt/nomad/alloc/5678/vm/local/downloads"},
	}

	if !slices.Equal(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}
