package bridge

import (
	"os"
	"path/filepath"
	"testing"
)

// A staged download moved to a folder that already holds its name takes the
// next "name (n).ext" Chrome would give it, and never replaces the old file.
func TestMoveUniqueRenamesInsteadOfOverwriting(t *testing.T) {
	stage, dest := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(dest, "report.pdf"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i, want := range []string{"report (1).pdf", "report (2).pdf"} {
		src := filepath.Join(stage, "guid-"+want)
		if err := os.WriteFile(src, []byte{byte('a' + i)}, 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := moveUnique(src, dest, "report.pdf")
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(got) != want {
			t.Errorf("moved to %s, want %s", filepath.Base(got), want)
		}
		if _, err := os.Stat(src); !os.IsNotExist(err) {
			t.Errorf("staged file %s still there", src)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(dest, "report.pdf")); string(b) != "old" {
		t.Errorf("the existing report.pdf was overwritten: %q", b)
	}
}
