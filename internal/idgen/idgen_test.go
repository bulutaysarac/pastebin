package idgen

import (
	"strings"
	"testing"
)

func TestNewShapeAndAlphabet(t *testing.T) {
	for range 1000 {
		id := New()
		if len(id) != Length {
			t.Fatalf("len(%q) = %d, want %d", id, len(id), Length)
		}
		for _, c := range id {
			if !strings.ContainsRune(alphabet, c) {
				t.Fatalf("id %q contains %q, which is not base62", id, c)
			}
		}
	}
}

func TestNewDoesNotRepeatInPractice(t *testing.T) {
	seen := make(map[string]struct{}, 100_000)
	for range 100_000 {
		id := New()
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %q within 100k draws", id)
		}
		seen[id] = struct{}{}
	}
}
