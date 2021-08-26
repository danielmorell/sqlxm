package backends

import (
	"database/sql"

	"github.com/jmoiron/sqlx"
)

type Postgres struct {
	// The database connection to use for this backend.
	db *sqlx.DB
	// The migration table name
	migrationTable string
}

// Setup does the initial configuration of the backend.
func (p *Postgres) Setup(table string, db *sqlx.DB) {
	p.migrationTable = table
	p.db = db
}

// InsertRecord migration record into the DB.
func (p *Postgres) InsertRecord(tx *sql.Tx, name string, hash string, comment string) error {
	s := nameTable(`INSERT INTO ?? (name, hash, comment) VALUES ($1, $2, $3);`, p.migrationTable)

	_, err := tx.Exec(s, name, hash, comment)

	return err
}

// HasMigrationTable returns true if the migration table exists.
func (p *Postgres) HasMigrationTable() bool {
	q := nameTable(`SELECT EXISTS(
		SELECT *
		FROM information_schema.tables
		WHERE
		  table_schema = 'public' AND
          table_name = '??'
	);`, p.migrationTable)

	exists := false
	err := p.db.Get(&exists, q)

	// If this query fails something has gone terribly wrong.
	if err != nil {
		panic(err)
	}
	return exists
}

// QueryPrevious queries and sets the records of all previous migrations.
func (p *Postgres) QueryPrevious() (map[string]string, error) {
	q := nameTable(`SELECT name, hash FROM ??;`, p.migrationTable)
	mr := make([]MigrationRecord, 0, 10)

	err := p.db.Select(&mr, q)
	if err != nil {
		return nil, err
	}

	prev := make(map[string]string)
	for _, r := range mr {
		prev[r.Name] = r.Hash
	}

	return prev, nil
}

// CreateMigrationTable makes the migrations table, and return the query used to
// do it.
func (p *Postgres) CreateMigrationTable() (string, error) {
	q := nameTable(`CREATE TABLE ??
	(
		id      SERIAL                     NOT NULL,
		name    VARCHAR(64)                NOT NULL,
		hash    VARCHAR(32)                NOT NULL,
		date    TIMESTAMP    DEFAULT NOW() NOT NULL,
        comment VARCHAR(512)               NOT NULL
	);
	
	COMMENT ON TABLE ?? IS 'list the schema changes';
	
	CREATE UNIQUE INDEX ??_id_uindex ON ?? (id);
	
	CREATE UNIQUE INDEX ??_name_uindex ON ?? (name);
	
	ALTER TABLE ?? 
		ADD CONSTRAINT ??_pk PRIMARY KEY (id);`, p.migrationTable)

	_, err := p.db.Exec(q)

	return q, err
}
