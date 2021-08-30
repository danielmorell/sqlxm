package backends

import (
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

type Backend interface {
	// Setup does the initial configuration of the backend.
	Setup(db *sqlx.DB, table string, tableSchema string)
	// InsertRecord migration record into the DB.
	InsertRecord(tx *sqlx.Tx, name string, hash string, comment string) error
	// HasMigrationTable returns true if the migration table exists.
	HasMigrationTable() (bool, error)
	// QueryPrevious queries and sets the records of all previous migrations.
	QueryPrevious() (map[string]string, error)
	// CreateMigrationTable makes the migrations table, and return the query used to
	// do it.
	CreateMigrationTable() (string, error)
	RepairHashes(tx *sqlx.Tx, hashes map[string]string) error
}

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

func InsertRecord(tx *sqlx.Tx, query string, args ...interface{}) error {
	_, err := tx.Exec(query, args...)
	return err
}

func HasMigrationTable(db *sqlx.DB, query string) (bool, error) {
	exists := false
	err := db.Get(&exists, query)

	// If this query fails something has gone terribly wrong.
	if err != nil {
		return false, err
	}
	return exists, nil
}

// QueryPrevious runs the query from the Backend.QueryPrevious and returns the
// results.
func QueryPrevious(db *sqlx.DB, query string) (map[string]string, error) {
	mr := make([]MigrationRecord, 0, 10)

	err := db.Select(&mr, query)
	if err != nil {
		return nil, err
	}

	prev := make(map[string]string)
	for _, r := range mr {
		prev[r.Name] = r.Hash
	}

	return prev, nil
}

func CreateMigrationTable(db *sqlx.DB, query string) (string, error) {
	_, err := db.Exec(query)

	return query, err
}

func RepairHashes(tx *sqlx.Tx, query string, hashes map[string]string) error {
	for name, hash := range hashes {
		if hash == "" {
			continue
		}
		_, err := tx.Exec(query, hash, name)
		if err != nil {
			return err
		}
	}

	return nil
}
