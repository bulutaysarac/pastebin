package environments

import (
	"os"
	"testing"
	"time"
)

// unsetenv removes key for this test and restores it afterwards.
func unsetenv(t *testing.T, key string) {
	t.Helper()
	t.Setenv(key, "") // registers the restore
	os.Unsetenv(key)
}

func wantPanic(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Errorf("%s: want a panic", name)
		}
	}()
	f()
}

func TestGetEnvInt(t *testing.T) {
	unsetenv(t, "PASTEBIN_TEST_INT")
	if got := GetEnvInt("PASTEBIN_TEST_INT", 30, 0); got != 30 {
		t.Errorf("unset: got %d, want the default 30", got)
	}

	t.Setenv("PASTEBIN_TEST_INT", "0")
	if got := GetEnvInt("PASTEBIN_TEST_INT", 30, 0); got != 0 {
		t.Errorf("0 with minimum 0: got %d, want 0", got)
	}
	wantPanic(t, "0 with minimum 1", func() { GetEnvInt("PASTEBIN_TEST_INT", 30, 1) })

	for _, bad := range []string{"", "-1", "thirty", "1.5"} {
		t.Setenv("PASTEBIN_TEST_INT", bad)
		wantPanic(t, bad, func() { GetEnvInt("PASTEBIN_TEST_INT", 30, 0) })
	}
}

func TestGetEnvDuration(t *testing.T) {
	unsetenv(t, "PASTEBIN_TEST_DURATION")
	if got := GetEnvDuration("PASTEBIN_TEST_DURATION", time.Minute); got != time.Minute {
		t.Errorf("unset: got %v, want the default 1m", got)
	}

	t.Setenv("PASTEBIN_TEST_DURATION", "90s")
	if got := GetEnvDuration("PASTEBIN_TEST_DURATION", time.Minute); got != 90*time.Second {
		t.Errorf("90s: got %v", got)
	}

	for _, bad := range []string{"", "0", "-1m", "5", "soon"} {
		t.Setenv("PASTEBIN_TEST_DURATION", bad)
		wantPanic(t, bad, func() { GetEnvDuration("PASTEBIN_TEST_DURATION", time.Minute) })
	}
}

func TestDatabasePoolDefaults(t *testing.T) {
	for _, key := range []string{"DB_MAX_OPEN_CONNS", "DB_MAX_IDLE_CONNS", "DB_CONN_MAX_LIFETIME"} {
		unsetenv(t, key)
	}
	if d := GetDatabase(); d.MaxOpenConns != 50 || d.MaxIdleConns != 50 || d.ConnMaxLifetime != 5*time.Minute {
		t.Errorf("defaults: open %d, idle %d, lifetime %v; want 50, 50, 5m", d.MaxOpenConns, d.MaxIdleConns, d.ConnMaxLifetime)
	}

	t.Setenv("DB_MAX_OPEN_CONNS", "20")
	if d := GetDatabase(); d.MaxIdleConns != 20 {
		t.Errorf("idle follows max open: got %d, want 20", d.MaxIdleConns)
	}

	t.Setenv("DB_MAX_OPEN_CONNS", "0")
	wantPanic(t, "DB_MAX_OPEN_CONNS=0 (unlimited)", func() { GetDatabase() })

	t.Setenv("DB_MAX_OPEN_CONNS", "20")
	t.Setenv("DB_MAX_IDLE_CONNS", "0")
	wantPanic(t, "DB_MAX_IDLE_CONNS=0 (keep none)", func() { GetDatabase() })
}
