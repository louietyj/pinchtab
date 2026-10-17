package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type composeImageService struct {
	Image string `yaml:"image"`
	Build any    `yaml:"build"`
}

func imageOnlyServicesOutsideTheirBuilderPrefix(t *testing.T, data []byte) []string {
	t.Helper()
	var file struct {
		Services map[string]composeImageService `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	builders := map[string]string{}
	for name, svc := range file.Services {
		if svc.Build != nil && svc.Image != "" {
			builders[svc.Image] = name
		}
	}
	var bad []string
	for name, svc := range file.Services {
		if svc.Build != nil {
			continue
		}
		builder, local := builders[svc.Image]
		if local && (builder != pinchtabImageBuilder || !strings.HasPrefix(name, pinchtabImageBuilder+"-")) {
			bad = append(bad, name+" reuses "+svc.Image+" built by "+builder)
		}
	}
	return bad
}

func TestEveryImageOnlyE2EServiceIsAPinchtabVariantSoItsBuilderIsAdded(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join("..", "..", "..", "..", "e2e", "docker-compose*.yml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) < 2 {
		t.Fatalf("found %d e2e compose files, want at least 2; the check would pass vacuously", len(paths))
	}
	variants := 0
	for _, path := range paths {
		data, err := os.ReadFile(path) // #nosec G304 -- the repo's own compose files
		if err != nil {
			t.Fatal(err)
		}
		variants += strings.Count(string(data), "\n  "+pinchtabImageBuilder+"-")
		for _, svc := range imageOnlyServicesOutsideTheirBuilderPrefix(t, data) {
			t.Errorf("%s: %s; servicesToBuild adds only the %q builder for %q-prefixed variants, so this service would run a stale image", path, svc, pinchtabImageBuilder, pinchtabImageBuilder)
		}
	}
	if variants == 0 {
		t.Fatal("no pinchtab-* variant found in the e2e compose files; the check would pass vacuously")
	}
}

func TestTheImageBuilderCheckSeesAVariantNamedOutsideThePrefix(t *testing.T) {
	const planted = `x-base: &base
  image: e2e-pinchtab:latest
services:
  pinchtab:
    <<: *base
    build: {context: .}
  pinchtab-secure:
    <<: *base
  hardened:
    <<: *base
  fixtures:
    image: nginx:alpine
`
	bad := imageOnlyServicesOutsideTheirBuilderPrefix(t, []byte(planted))

	if len(bad) != 1 || !strings.HasPrefix(bad[0], "hardened ") {
		t.Fatalf("check flagged %v, want only the anchor-inherited variant named outside the prefix", bad)
	}
}
