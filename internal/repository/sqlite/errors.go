package sqlite

import (
	"errors"

	sqllib "modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// isSQLiteUniqueViolation returns true when err is a SQLite UNIQUE constraint error.
func isSQLiteUniqueViolation(err error) bool {
	var sqliteErr *sqllib.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}

// isSQLiteForeignKeyViolation returns true when err is a SQLite FOREIGN KEY constraint error.
func isSQLiteForeignKeyViolation(err error) bool {
	var sqliteErr *sqllib.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY
}
