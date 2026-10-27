package mysqlstore

import (
	"errors"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/bulutaysarac/pastebin/internal/paste"
)

func TestTranslate(t *testing.T) {
	if err := translate(nil); err != nil {
		t.Errorf("nil: got %v", err)
	}
	if err := translate(&mysql.MySQLError{Number: errDuplicateEntry}); !errors.Is(err, paste.ErrIDTaken) {
		t.Errorf("duplicate entry: got %v, want ErrIDTaken", err)
	}
	for _, n := range []uint16{errLockWaitTimeout, errQueryTimeout} {
		err := translate(&mysql.MySQLError{Number: n})
		var myErr *mysql.MySQLError
		if !errors.Is(err, paste.ErrStoreTimeout) || !errors.As(err, &myErr) || myErr.Number != n {
			t.Errorf("error %d: got %v, want ErrStoreTimeout wrapping the MySQL error", n, err)
		}
	}
	other := &mysql.MySQLError{Number: 1146} // table doesn't exist
	if err := translate(other); err != other {
		t.Errorf("other errors pass through: got %v", err)
	}
}

func TestStatementTimeoutIsWholeSeconds(t *testing.T) {
	// lock_wait_timeout and innodb_lock_wait_timeout only take whole seconds.
	if StatementTimeout < time.Second || StatementTimeout%time.Second != 0 {
		t.Fatalf("StatementTimeout = %v, want a whole number of seconds", StatementTimeout)
	}
}
