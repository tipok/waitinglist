package sqlite

import (
	"fmt"
	"time"
)

var sqliteTimeFormats = []string{
	"2006-01-02T15:04:05Z",
	"2006-01-02T15:04:05.999999999Z",
	"2006-01-02 15:04:05",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05Z07:00",
}

func parseSQLiteTime(s string) (time.Time, error) {
	for _, layout := range sqliteTimeFormats {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("cannot parse %q as time", s)
}

// timeScanner scans a SQLite TEXT timestamp into a *time.Time.
type timeScanner struct{ t *time.Time }

func (s *timeScanner) Scan(v any) error {
	if v == nil {
		return fmt.Errorf("timeScanner: unexpected NULL for non-nullable column")
	}
	str, ok := v.(string)
	if !ok {
		return fmt.Errorf("timeScanner: unexpected type %T", v)
	}
	t, err := parseSQLiteTime(str)
	if err != nil {
		return err
	}
	*s.t = t
	return nil
}

// nullTimeScanner scans a nullable SQLite TEXT timestamp into a **time.Time.
type nullTimeScanner struct{ t **time.Time }

func (s *nullTimeScanner) Scan(v any) error {
	if v == nil {
		*s.t = nil
		return nil
	}
	str, ok := v.(string)
	if !ok {
		return fmt.Errorf("nullTimeScanner: unexpected type %T", v)
	}
	t, err := parseSQLiteTime(str)
	if err != nil {
		return err
	}
	*s.t = &t
	return nil
}
