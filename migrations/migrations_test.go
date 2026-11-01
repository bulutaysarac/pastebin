package migrations

import (
	"fmt"
	"io/fs"
	"strings"
	"testing"
)

// goose parses a file only when it applies it, so these checks are what can
// be caught without a database.
func TestFilesAreNumberedInOrderAndMarkedUp(t *testing.T) {
	names, err := fs.Glob(FS, "*.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 {
		t.Fatal("no migrations embedded")
	}
	for i, name := range names {
		if want := fmt.Sprintf("%05d_", i+1); !strings.HasPrefix(name, want) {
			t.Errorf("%s: want the prefix %s, with no gaps or repeats", name, want)
		}
		body, err := fs.ReadFile(FS, name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(body), "-- +goose Up\n") {
			t.Errorf("%s: must start with \"-- +goose Up\"", name)
		}
	}
}
