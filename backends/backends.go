package backends

import (
	"strings"
	"time"
)

type MigrationRecord struct {
	ID      int       `db:"id"`
	Name    string    `db:"name"`
	Hash    string    `db:"hash"`
	Date    time.Time `db:"date"`
	Comment string    `db:"comment"`
}

// nameTable takes a query and replaces all instances of "??" with the tableName
func nameTable(query string, tableName string) string {
	return strings.Replace(query, "??", tableName, -1)
}
