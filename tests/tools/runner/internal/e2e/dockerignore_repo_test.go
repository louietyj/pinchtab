package e2e

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheRepoDockerignoreKeepsLocalToolchainsAndBuildOutputsOutOfTheContext(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "..", dockerIgnoreFile)

	excluded, err := contextExcludedDirs(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{".tools", "dist"} {
		if !excluded[dir] {
			t.Errorf("%s does not exclude %s/; the build context ships it and the smoke-image digest follows its contents", path, dir)
		}
	}

	file, err := os.Open(path) // #nosec G304 -- the repo's own .dockerignore
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	entries := map[string]bool{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		entries[strings.TrimSpace(scanner.Text())] = true
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if !entries["pinchtab-dev"] {
		t.Errorf("%s does not exclude the pinchtab-dev binary", path)
	}
}
