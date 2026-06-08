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

// isSQLiteForeignKeyViolation returns true when err is a SQLite FOREIGN KEY constraint error (code 787).
func isSQLiteForeignKeyViolation(err error) bool {
	var sqliteErr *sqllib.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == 787
}
