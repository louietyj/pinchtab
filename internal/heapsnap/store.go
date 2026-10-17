package heapsnap

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	Ext      = ".heapsnapshot"
	DirName  = "heapsnapshots"
	IDPrefix = "heap_"
)

var ErrInvalidID = errors.New("invalid heap snapshot id")

var validID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

func Dir(stateDir string) string {
	return filepath.Join(stateDir, DirName)
}

func IDFromPath(path string) string {
	return strings.TrimSuffix(filepath.Base(path), Ext)
}

func PathForID(dir, id string) (string, error) {
	if !validID.MatchString(id) {
		return "", fmt.Errorf("%w: %q", ErrInvalidID, id)
	}
	return filepath.Join(dir, id+Ext), nil
}
