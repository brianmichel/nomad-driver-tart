package driver

import (
	"reflect"
	"testing"
)

func TestBuildRootDiskArgs_Nil(t *testing.T) {
	got, err := buildRootDiskArgs(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no args for nil options, got %v", got)
	}
}

func TestBuildRootDiskArgs_ReadOnly(t *testing.T) {
	cfg := &RootDiskOptions{ReadOnly: true}
	got, err := buildRootDiskArgs(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--root-disk-opts=ro"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBuildRootDiskArgs_CachingModes_Normalized(t *testing.T) {
	cases := map[string]string{
		"automatic":   "automatic",
		"UNCACHED":    "uncached",
		"  cached   ": "cached",
	}
	for input, exp := range cases {
		in := input // capture range variable
		cfg := &RootDiskOptions{CachingMode: &in}
		got, err := buildRootDiskArgs(cfg)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", input, err)
		}
		want := []string{"--root-disk-opts=caching=" + exp}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %v, want %v", input, got, want)
		}
	}
}

func TestBuildRootDiskArgs_SyncModes_Normalized(t *testing.T) {
	cases := map[string]string{
		"none":    "none",
		"FSYNC":   "fsync",
		"  full ": "full",
	}
	for input, exp := range cases {
		in := input // capture range variable
		cfg := &RootDiskOptions{SyncMode: &in}
		got, err := buildRootDiskArgs(cfg)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", input, err)
		}
		want := []string{"--root-disk-opts=sync=" + exp}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: got %v, want %v", input, got, want)
		}
	}
}

func TestBuildRootDiskArgs_AllOptions(t *testing.T) {
	cache := "cached"
	sync := "full"
	cfg := &RootDiskOptions{ReadOnly: true, CachingMode: &cache, SyncMode: &sync}
	got, err := buildRootDiskArgs(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"--root-disk-opts=ro,caching=cached,sync=full"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
