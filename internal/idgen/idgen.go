// Package idgen produces the short IDs that become paste URLs.
package idgen

import "crypto/rand"

const (
	alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

	// Length is the number of characters in an ID: 62^8 ≈ 2.18e14 possible values.
	Length = 8
)

// New returns a random base62 ID of Length characters.
//
// Uniqueness is not guaranteed here. The store enforces it through the
// primary key, and the caller retries on a collision.
func New() string {
	out := make([]byte, 0, Length)
	buf := make([]byte, Length*2)
	for len(out) < Length {
		rand.Read(buf) // never returns an error since Go 1.24; it crashes instead
		for _, b := range buf {
			// 248 = 4*62. Skipping bytes >= 248 keeps all 62 characters equally
			// likely; a plain b%62 would favour the first 8.
			if b >= 248 {
				continue
			}
			out = append(out, alphabet[b%62])
			if len(out) == Length {
				break
			}
		}
	}
	return string(out)
}
