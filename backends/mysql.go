package backends

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

type MySQL struct {
	// The database connection to use for this backend.
	db *sqlx.DB
	// The migration table name
	table string
	// The SQL 'table_schema' in MySQL is the name of the DB.
	tableSchema string
}

// Setup does the initial configuration of the backend.
func (m *MySQL) Setup(db *sqlx.DB, table string, tableSchema string) {
	m.db = db
	m.table = table
	m.tableSchema = tableSchema
}

// InsertRecord migration record into the DB.
func (m *MySQL) InsertRecord(tx *sqlx.Tx, name string, hash string, comment string) error {
	q := nameTable(`INSERT INTO ?? (name, hash, comment) VALUES (?, ?, ?);`, m.table)

	return InsertRecord(tx, q, name, hash, comment)
}

// HasMigrationTable returns true if the migration table exists.
func (m *MySQL) HasMigrationTable() (bool, error) {
	q := fmt.Sprintf(`SELECT EXISTS(
		SELECT * FROM information_schema.tables 
		WHERE table_schema = '%s' 
		AND table_name = '%s'
	);`, m.tableSchema, m.table)
	return HasMigrationTable(m.db, q)
}

// QueryPrevious queries and sets the records of all previous migrations.
func (m *MySQL) QueryPrevious() (map[string]string, error) {
	q := nameTable(`SELECT name, hash FROM ??;`, m.table)
	return QueryPrevious(m.db, q)
}

// CreateMigrationTable makes the migrations table, and return the query used to
// do it.
func (m *MySQL) CreateMigrationTable() (string, error) {
	q := nameTable(`CREATE TABLE ?? (
		id      INT                        NOT NULL AUTO_INCREMENT PRIMARY KEY,
		name    VARCHAR(64)                NOT NULL UNIQUE KEY,
		hash    VARCHAR(32)                NOT NULL,
		date    TIMESTAMP    DEFAULT NOW() NOT NULL,
        comment VARCHAR(512)               NOT NULL
	)
	COMMENT 'list the schema changes';`, m.table)

	return CreateMigrationTable(m.db, q)
}

func (m *MySQL) RepairHashes(tx *sqlx.Tx, hashes map[string]string) error {
	q := nameTable(`UPDATE ?? SET hash = ? WHERE name = ?`, m.table)
	return RepairHashes(tx, q, hashes)
}
