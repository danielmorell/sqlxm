package sqlxm

import (
	"crypto/md5"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// Migration is a single schema change to apply to the database.
type Migration struct {
	Name      string
	Comment   string
	hash      string
	Statement string
	args      []interface{}
	migrated  bool
}

// Execute the migration on the database
func (m Migration) run(tx *sql.Tx) error {
	_, err := tx.Exec(m.Statement, m.args...)
	return err
}

// Insert the migration record row into the migration table
func (m Migration) insertRecord(tx *sql.Tx, migrator *Migrator) error {
	s := migrator.db.Rebind(migrator.name(`INSERT INTO ?? (name, hash, comment) VALUES (?, ?, ?);`))

	_, err := tx.Exec(s, m.Name, fmt.Sprintf("%x", m.hash), m.Comment)

	return err
}

type MigrationRecord struct {
	ID      int       `db:"id"`
	Name    string    `db:"name"`
	Hash    string    `db:"hash"`
	Date    time.Time `db:"date"`
	Comment string    `db:"comment"`
}

type MigrationLog struct {
	Name    string
	Hash    string
	Comment string
	Error   error
}

// Migrator handles the process of migrating your database. Each instance of
// Migrator represents a single database that should have schema migrations
// applied to it.
type Migrator struct {
	// The database connection to use for this Migrator.
	db *sqlx.DB
	// The name of the database table to use for migration records.
	TableName string
	// All the migration records for the database.
	migrations []Migration
	log        []MigrationLog
}

// The AddMigration method adds a new Migration to the list of migrations needed.
//
// It is important to note that the name argument must be unique, and it is used
// to identify each migration. Changing the value of an existing migration name
// will likely cause errors. However, since a migration can be any SQL
// statement, you can use a new migration to change the name of an existing
// migration record in the DB. However, this is strongly discouraged, as it is
// easy to introduce an error state that will require manual edits to your
// migration table to fix.
func (m *Migrator) AddMigration(name string, comment string, statement string, args ...interface{}) {
	if m.migrations == nil {
		m.migrations = make([]Migration, 0, 1)
	}
	mig := Migration{
		Name:      name,
		Comment:   comment,
		hash:      hashQuery(statement, args),
		Statement: statement,
		args:      args,
		migrated:  false,
	}

	m.migrations = append(m.migrations, mig)
}

// Run executes the new migrations against the DB.
func (m *Migrator) Run() ([]MigrationLog, error) {
	err := m.run()
	return m.log, err
}

// run each Migration in the Migrator.
func (m *Migrator) run() error {
	success := true
	// Create transaction
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if success {
			tx.Commit()
			return
		}
		tx.Rollback()
	}()

	// Create the migration table if it does not exist
	if !m.hasMigrationTable() {
		q := m.migrationTableStmt()
		_, err = tx.Exec(q)
		m.log = append(m.log, MigrationLog{
			Name:    fmt.Sprintf("create_%s_table", m.TableName),
			Hash:    hashQuery(q),
			Comment: fmt.Sprintf("created '%s' table", m.TableName),
			Error:   err,
		})
		if err != nil {
			success = false
			return err
		}
	}

	// Get previous migrations
	previous := []MigrationRecord{}

	err = m.db.Select(previous, m.name(`SELECT name, hash FROM ??`))
	if err != nil {
		return err
	}

	// Run each migration
	for _, mig := range m.migrations {
		err = mig.run(tx)
		// If the migration record insert fails something is wrong, and we should stop.
		m.log = append(m.log, MigrationLog{
			Name:    mig.Name,
			Hash:    mig.hash,
			Comment: mig.Comment,
			Error:   err,
		})

		if err != nil {
			success = false
			break
		}

		err = mig.insertRecord(tx, m)
		if err != nil {
			return err
		}
	}

	return err
}

// hasMigrationTable checks to see if the migration table exists already.
func (m Migrator) hasMigrationTable() bool {
	q := m.name(`SELECT EXISTS(
		SELECT *
		FROM information_schema.tables
		WHERE
		  table_schema = 'public' AND
          table_name = '??'
	);`)

	exists := false
	err := m.db.Get(&exists, q)

	// If this query fails something has gone terribly wrong.
	if err != nil {
		panic(err)
	}
	return exists
}

// queryRecord returns the records of all migrations.
func (m Migrator) queryRecords() ([]MigrationRecord, error) {
	q := m.name(`SELECT id, name, hash, time FROM ?? ORDER BY "time"`)
	mr := []MigrationRecord{}
	var err error = nil
	err = m.db.Select(&mr, q)
	if err != nil {
		return nil, err
	}
	return mr, nil
}

// Returns the create migration table statement.
func (m Migrator) migrationTableStmt() string {
	return m.name(`CREATE TABLE ??
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
		ADD CONSTRAINT ??_pk PRIMARY KEY (id);`)
}

func (m Migrator) name(query string) string {
	return strings.Replace(query, "??", m.TableName, -1)
}

func New(db *sqlx.DB, tableName string) Migrator {
	return Migrator{
		db:        db,
		TableName: tableName,
	}
}

// The hashQuery function is for creating a checksum for each migration.
func hashQuery(query string, args ...interface{}) string {
	var b strings.Builder
	b.WriteString(query)
	for _, arg := range args {
		b.WriteString(fmt.Sprintf("%v", arg))
	}
	return fmt.Sprintf("%x", md5.Sum([]byte(b.String())))
}
