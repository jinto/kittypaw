package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReleaseConfigTargetsKittypawOrg(t *testing.T) {
	root := filepath.Join("..")
	checks := map[string][]string{
		filepath.Join(root, ".goreleaser.yml"): {
			"owner: kittypaw-app",
			"name: kitty",
			"https://raw.githubusercontent.com/kittypaw-app/kitty/main/install.sh",
		},
		filepath.Join(root, "install.sh"): {
			`REPO="kittypaw-app/kitty"`,
		},
	}

	for path, wants := range checks {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		content := string(b)
		if strings.Contains(content, "jinto/kittypaw") {
			t.Fatalf("%s still points release/download metadata at jinto/kittypaw", path)
		}
		for _, want := range wants {
			if !strings.Contains(content, want) {
				t.Fatalf("%s missing %q", path, want)
			}
		}
	}
}
