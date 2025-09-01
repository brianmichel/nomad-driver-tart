package driver

import (
	"reflect"
	"testing"
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
	if !reflect.DeepEqual(got, want) {
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
	if !reflect.DeepEqual(got, want) {
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
	if !reflect.DeepEqual(got, want) {
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
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildDirectoryArgs_RequiresPath(t *testing.T) {
	dirs := []DirectoryMount{{}}
	if _, err := buildDirectoryArgs(dirs); err == nil {
		t.Fatalf("expected error for empty path, got nil")
	}
}
