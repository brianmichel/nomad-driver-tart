package driver

import "testing"

func TestRegistryHost(t *testing.T) {
	cases := map[string]string{
		"708167139547.dkr.ecr.us-east-2.amazonaws.com/ci-runner-macos-sequoia:latest": "708167139547.dkr.ecr.us-east-2.amazonaws.com",
		"ghcr.io/owner/repo:tag":          "ghcr.io",
		"https://gcr.io/owner/repo:tag":   "gcr.io",
		"docker.io/library/ubuntu:latest": "docker.io",
	}

	for input, expected := range cases {
		got, err := registryHost(input)
		if err != nil {
			t.Fatalf("%s returned error: %v", input, err)
		}
		if got != expected {
			t.Fatalf("expected %s got %s", expected, got)
		}
	}
}
