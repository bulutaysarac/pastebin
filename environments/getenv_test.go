package environments

import "testing"

func TestGetEnvInt(t *testing.T) {
	if got := GetEnvInt("PASTEBIN_TEST_UNSET", 30); got != 30 {
		t.Errorf("unset: got %d, want the default 30", got)
	}

	t.Setenv("PASTEBIN_TEST_INT", "0")
	if got := GetEnvInt("PASTEBIN_TEST_INT", 30); got != 0 {
		t.Errorf("0: got %d, want 0", got)
	}

	for _, bad := range []string{"", "-1", "thirty", "1.5"} {
		t.Setenv("PASTEBIN_TEST_INT", bad)
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%q: want a panic", bad)
				}
			}()
			GetEnvInt("PASTEBIN_TEST_INT", 30)
		}()
	}
}
