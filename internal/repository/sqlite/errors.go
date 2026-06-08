package sqlite

import (
	"errors"

	sqllib "modernc.org/sqlite"
)

// isSQLiteUniqueViolation returns true when err is a SQLite UNIQUE constraint error (code 2067).
func isSQLiteUniqueViolation(err error) bool {
	var sqliteErr *sqllib.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == 2067
}
