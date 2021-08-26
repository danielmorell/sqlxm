package sqlxm

import (
	"crypto/md5"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/danielmorell/sqlxm/backends"
	"github.com/jmoiron/sqlx"
)

const (
	SUCCESS = iota
	PREVIOUS
	ERROR
	ERROR_HASH
)

var defaultBackends = map[string][]string{
	"postgres":  {"postgres", "pgx", "pq-timeouts", "cloudsqlpostgres", "ql", "nrpostgres", "cockroach"},
	"mysql":     {"mysql", "nrmysql"},
	"sqlite":    {"sqlite3", "nrsqlite3"},
	"oracle":    {"oci8", "ora", "goracle", "godror"},
	"sqlserver": {"sqlserver"},
}

var backendMap sync.Map

func init() {
	for db, drivers := range defaultBackends {
		for _, driver := range drivers {
			backendMap.Store(driver, db)
		}
	}
}

// BackendType returns the backend key for a given database given a driverName.
func BackendType(driverName string) string {
	itype, ok := backendMap.Load(driverName)
	if !ok {
		return "unknown"
	}
	return itype.(string)
}

var registeredBackends = map[string]Backend{
	"postgres": &backends.Postgres{},
}

// RegisterBackend adds a new DB Backend to SQLXM for Migrator to use to run
// queries. A backend handles peculiarities in SQL dialects and can help
// abstract alternate implementations.
func RegisterBackend(key string, backend Backend) error {
	_, exists := registeredBackends[key]
	if exists {
		return errors.New(fmt.Sprintf("backend with key '%s' already exists", key))
	}
	registeredBackends[key] = backend
	return nil
}

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
	return migrator.backend.InsertRecord(tx, m.Name, m.hash, m.Comment)
}

type MigrationLog struct {
	Name    string
	Hash    string
	Status  int
	Details string
}

type Backend interface {
	// Setup does the initial configuration of the backend.
	Setup(table string, db *sqlx.DB)
	// InsertRecord migration record into the DB.
	InsertRecord(tx *sql.Tx, name string, hash string, comment string) error
	// HasMigrationTable returns true if the migration table exists.
	HasMigrationTable() bool
	// QueryPrevious queries and sets the records of all previous migrations.
	QueryPrevious() (map[string]string, error)
	// CreateMigrationTable makes the migrations table, and return the query used to
	// do it.
	CreateMigrationTable() (string, error)
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
	// The log of all migrations that have been run or attempted to be run.
	log []MigrationLog
	// Previous migrations to make sure we don't run them twice.
	previous map[string]string
	// Strict mode stops migrations and returns an error if the hashes don't
	// match for a migration.
	strict bool
	// The names of migrations that need the hash repaired.
	repair map[string]bool
	// Set of added migrations
	names map[string]bool
	// The query runner for the db.
	backend Backend
}

func (m *Migrator) UseBackend(key string) error {
	b, ok := registeredBackends[key]
	if !ok {
		return errors.New(fmt.Sprintf("backend '%s' is not a registered backend", key))
	}
	m.backend = b
	m.backend.Setup(m.TableName, m.db)
	return nil
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
//
// An error is returned if a migration with the same name has already been
// added.
func (m *Migrator) AddMigration(name string, comment string, statement string, args ...interface{}) error {
	if m.names[name] {
		return errors.New(fmt.Sprintf("migration '%s' alraedy exists", name))
	}
	// Add name to set
	m.names[name] = true

	// Create the new migration
	mig := Migration{
		Name:      name,
		Comment:   comment,
		hash:      hashQuery(statement, args),
		Statement: statement,
		args:      args,
		migrated:  false,
	}

	m.migrations = append(m.migrations, mig)
	return nil
}

// RepairHash finds an existing migration by name and updates the hash in the
// DB. This is useful if you are using RunStrict, and there have been
// non-substantive changes to the Migration.Statement such as formatting or
// indenting changes.
//
// The hash for each name supplied will be updated.
//
// Hash repairs will be run just before the migrations and will not be applied
// if the migrations fail.
func (m *Migrator) RepairHash(names ...string) {
	for _, n := range names {
		m.repair[n] = true
	}
}

// Run executes the new migrations against the DB.
//
// Run does a couple of things...
//
//    1. Creates the migration table if it does not exist.
//    2. Repairs any hashes that need to be updated.
//    3. Executes each new migration in the order they were added.
//    4. Adds each now migration record to the migration table.
//    5. Returns a log of all migrations.
//
// All the migrations are run as a single transaction. If a migration fails or
// an error is encountered, an error is returned and none of the migrations are
// applied. This ensures that if something goes wrong there is not an unknown
// state where some migrations are applied and some are not.
//
// It is important to note that Run does not validate the integrity of past
// migrations. Once a migration has been run the hash is stored in the DB but
// the hash is not checked again. This means that if changes are made to a
// migration statement after it has already been run, the expected state of the
// DB and the state after migrations have been run may be different.
//
// The logic for not checking the hash on each subsequent run is simple. "alter
// table" and "ALTER TABLE" produce the same results, but have a different hash.
// To keep auto-formatters and linter changes from breaking old migrations Run
// will ignore these and all other changes to the statement and args.
//
// If you want to validate the hash you can use RunStrict instead. If you chose
// RunStrict you may need to use the RepairHash method to manually update
// migrations that had non-substantive changes.
func (m *Migrator) Run() ([]MigrationLog, error) {
	m.strict = false
	err := m.run()
	return m.log, err
}

// RunStrict executes the new migrations against the DB like Run, but in strict
// mode. Strict mode causes the migrations to fail if an existing record hash
// does not match the hash of the migration.
func (m *Migrator) RunStrict() ([]MigrationLog, error) {
	m.strict = true
	err := m.run()
	return m.log, err
}

// run all the Migrator.migrations.
func (m *Migrator) run() error {
	commit := true

	// Create the migration table if it does not exist
	if !m.backend.HasMigrationTable() {
		err := m.createMigrationTable()
		if err != nil {
			return err
		}
	}

	// Create transaction for migrations
	tx, err := m.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if commit {
			tx.Commit()
			return
		}
		tx.Rollback()
	}()

	// Get previous migrations
	prev, err := m.backend.QueryPrevious()
	if err != nil {
		return err
	}
	m.previous = prev

	// Run each migration
	for _, mig := range m.migrations {
		err = m.executeMigration(tx, mig)
		if err != nil {
			return err
		}
	}

	return err
}

// Executes a single migration
func (m *Migrator) executeMigration(tx *sql.Tx, mig Migration) error {
	mLog := MigrationLog{
		Name:    mig.Name,
		Hash:    mig.hash,
		Status:  SUCCESS,
		Details: "ran migration successfully",
	}
	defer func() {
		m.log = append(m.log, mLog)
	}()

	hash, exists := m.previous[mig.Name]
	if exists {
		mLog.Status = PREVIOUS
		mLog.Details = "migration already run"
		if hash != mig.hash {
			d := fmt.Sprintf("hash mismatch DB: '%s' Migration: '%s'", hash, mig.hash)
			mLog.Details = d
			if m.strict {
				mLog.Status = ERROR_HASH
				return errors.New(fmt.Sprintf("%s %s", mig.Name, d))
			}
		}
		return nil
	}

	err := mig.run(tx)
	if err != nil {
		mLog.Status = ERROR
		mLog.Details = fmt.Sprintf("failed: %s", err)
		return err
	}

	// If the migration record insert fails something is wrong, and we should stop.
	err = mig.insertRecord(tx, m)
	if err != nil {
		mLog.Status = ERROR
		mLog.Details = fmt.Sprintf("record insert failed: %s", err)
		return err
	}
	return nil
}

// Creates the migrations table
func (m *Migrator) createMigrationTable() error {
	q, err := m.backend.CreateMigrationTable()

	l := MigrationLog{
		Name:    fmt.Sprintf("create_%s_table", m.TableName),
		Hash:    hashQuery(q),
		Status:  SUCCESS,
		Details: fmt.Sprintf("created '%s' table", m.TableName),
	}

	if err != nil {
		l.Status = ERROR
		l.Details = err.Error()
	}

	m.log = append(m.log, l)
	return err
}

// name takes a query and replaces all instances of "??" with the Migrator
// TableName
func (m Migrator) name(query string) string {
	return strings.Replace(query, "??", m.TableName, -1)
}

// New creates and returns a new Migrator instance. You typically should use one
// Migrator per database.
func New(db *sqlx.DB, tableName string) (Migrator, error) {
	m := Migrator{
		db:         db,
		TableName:  tableName,
		previous:   make(map[string]string),
		migrations: make([]Migration, 0, 1),
		repair:     make(map[string]bool),
		names:      make(map[string]bool),
	}
	b := BackendType(db.DriverName())
	err := m.UseBackend(b)

	return m, err
}

// The hashQuery function is for creating a checksum for each Migration.
func hashQuery(query string, args ...interface{}) string {
	var b strings.Builder
	b.WriteString(query)
	for _, arg := range args {
		b.WriteString(fmt.Sprintf("%v", arg))
	}
	return fmt.Sprintf("%x", md5.Sum([]byte(b.String())))
}
