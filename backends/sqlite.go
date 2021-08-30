package backends

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

type SQLite struct {
	// The database connection to use for this backend.
	db *sqlx.DB
	// The migration table name
	table string
}

// Setup does the initial configuration of the backend.
func (s *SQLite) Setup(db *sqlx.DB, table string, tableSchema string) {
	s.db = db
	s.table = table
}

// InsertRecord migration record into the DB.
func (s *SQLite) InsertRecord(tx *sqlx.Tx, name string, hash string, comment string) error {
	q := nameTable(`INSERT INTO ?? (name, hash, comment) VALUES (?, ?, ?);`, s.table)

	return InsertRecord(tx, q, name, hash, comment)
}

// HasMigrationTable returns true if the migration table exists.
func (s *SQLite) HasMigrationTable() (bool, error) {
	q := fmt.Sprintf(`SELECT count(name)
		FROM sqlite_master 
		WHERE type='table' 
		AND name = '%s';`, s.table)

	return HasMigrationTable(s.db, q)
}

// QueryPrevious queries and sets the records of all previous migrations.
func (s *SQLite) QueryPrevious() (map[string]string, error) {
	q := nameTable(`SELECT name, hash FROM ??;`, s.table)
	return QueryPrevious(s.db, q)
}

// CreateMigrationTable makes the migrations table, and return the query used to
// do it.
func (s *SQLite) CreateMigrationTable() (string, error) {
	q := nameTable(`CREATE TABLE ?? (
		-- list the schema changes
		id      INTEGER                             PRIMARY KEY,
		name    TEXT                                NOT NULL UNIQUE,
		hash    TEXT                                NOT NULL,
		date    TIMESTAMP DEFAULT CURRENT_TIMESTAMP NOT NULL,
        comment TEXT                                NOT NULL
	);`, s.table)

	return CreateMigrationTable(s.db, q)
}

func (s *SQLite) RepairHashes(tx *sqlx.Tx, hashes map[string]string) error {
	q := nameTable(`UPDATE ?? SET hash = ? WHERE name = ?`, s.table)
	return RepairHashes(tx, q, hashes)
}
