package backends

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

type Postgres struct {
	// The database connection to use for this backend.
	db *sqlx.DB
	// The migration table name
	table string
	// The SQL 'table_schema' usually is 'public'
	tableSchema string
}

// Setup does the initial configuration of the backend.
func (p *Postgres) Setup(db *sqlx.DB, table string, tableSchema string) {
	p.db = db
	p.table = table
	p.tableSchema = tableSchema
}

// InsertRecord migration record into the DB.
func (p *Postgres) InsertRecord(tx *sqlx.Tx, name string, hash string, comment string) error {
	q := nameTable(`INSERT INTO ?? (name, hash, comment) VALUES ($1, $2, $3);`, p.table)

	return InsertRecord(tx, q, name, hash, comment)
}

// HasMigrationTable returns true if the migration table exists.
func (p *Postgres) HasMigrationTable() (bool, error) {
	q := fmt.Sprintf(`SELECT EXISTS(
		SELECT * FROM information_schema.tables
		WHERE table_schema = '%s' 
		AND table_name = '%s'
	);`, p.tableSchema, p.table)
	return HasMigrationTable(p.db, q)
}

// QueryPrevious queries and sets the records of all previous migrations.
func (p *Postgres) QueryPrevious() (map[string]string, error) {
	q := nameTable(`SELECT name, hash FROM ??;`, p.table)
	return QueryPrevious(p.db, q)
}

// CreateMigrationTable makes the migrations table, and return the query used to
// do it.
func (p *Postgres) CreateMigrationTable() (string, error) {
	q := nameTable(`CREATE TABLE ?? (
		id      SERIAL
			CONSTRAINT ??_pk PRIMARY KEY,
		name    VARCHAR(64)                NOT NULL,
		hash    VARCHAR(32)                NOT NULL,
		date    TIMESTAMP    DEFAULT NOW() NOT NULL,
        comment VARCHAR(512)               NOT NULL
	);
	
	COMMENT ON TABLE ?? IS 'list the schema changes';
	
	CREATE UNIQUE INDEX ??_name_uindex ON ?? (name);`, p.table)
	return CreateMigrationTable(p.db, q)
}

func (p *Postgres) RepairHashes(tx *sqlx.Tx, hashes map[string]string) error {
	q := nameTable(`UPDATE ?? SET hash = $1 WHERE name = $2`, p.table)
	return RepairHashes(tx, q, hashes)
}
