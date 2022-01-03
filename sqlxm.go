package sqlxm

import (
	"crypto/md5"
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
	"postgres":  {"postgres", "pgx", "pq-timeouts", "cloudsqlpostgres", "nrpostgres", "cockroach"},
	"mysql":     {"mysql", "nrmysql"},
	"sqlite":    {"sqlite", "sqlite3", "nrsqlite3"},
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

var registeredBackends = map[string]backends.Backend{
	"mysql":    &backends.MySQL{},
	"postgres": &backends.Postgres{},
	"sqlite":   &backends.SQLite{},
}

// RegisterBackend adds a new DB Backend to SQLXM for Migrator to use to run
// queries. A backend handles peculiarities in SQL dialects and can help
// abstract alternate implementations.
func RegisterBackend(key string, backend backends.Backend) error {
	_, exists := registeredBackends[key]
	if exists {
		return fmt.Errorf("backend with key '%s' already exists", key)
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
func (m Migration) run(tx *sqlx.Tx) error {
	_, err := tx.Exec(m.Statement, m.args...)
	return err
}

// Insert the migration record row into the migration table
func (m Migration) insertRecord(tx *sqlx.Tx, migrator *Migrator) error {
	return migrator.backend.InsertRecord(tx, m.Name, m.hash, m.Comment)
}

// A MigrationLog represents the results from a single migration.
type MigrationLog struct {
	Name    string
	Hash    string
	Status  int
	Details string
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
	// safe mode stops migrations and returns an error if the hashes don't
	// match for a migration.
	safe bool
	// The names of migrations that need the hash repaired.
	repair map[string]string
	// Set of added migrations
	names map[string]struct{}
	// The query runner for the db.
	backend backends.Backend
	// The SQL 'table_schema' in Postgres this is typically 'public' in MySQL
	// this is the name of the DB.
	tableSchema string
}

// UseBackend changes the default backend to a custom or built-in backend. The
// backend must be registered before it can be used. A backend can be registered
// once and used on multiple migrator instances.
func (m *Migrator) UseBackend(key string) error {
	b, ok := registeredBackends[key]
	if !ok {
		return fmt.Errorf("backend '%s' is not a registered backend", key)
	}
	m.backend = b
	m.backend.Setup(m.db, m.TableName, m.tableSchema)
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
	if _, ok := m.names[name]; ok {
		return fmt.Errorf("migration '%s' alraedy exists", name)
	}
	// Add name to set
	m.names[name] = struct{}{}

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
// DB. This is useful if you are using Run in safe mode, and there have been
// non-substantive changes to the Migration.Statement such as formatting or
// indenting changes.
//
// The hash for each name supplied will be updated.
//
// Hash repairs will be run just before the migrations and will not be applied
// if the migrations fail.
func (m *Migrator) RepairHash(names ...string) {
	for _, n := range names {
		m.repair[n] = ""
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
// It is important to note that Run validates the integrity of past
// migrations. Once a migration has been run the hash is stored in the DB and
// the hash is checked against the migration hash each time it is run. This
// means that if changes are made to a migration statement after it has already
// been run, the two hashes will not match. In this case Run will return a hash
// mismatch error.
//
// The reason the hash is checked on each subsequent run is simple. Adding "NOT
// NULL" to an already run "CREATE TABLE" migration will cause that brand-new
// development database you just created to have the right column definition.
// However, the production database will still have that column defined as
// "nullable" since the migration is not run again. This can cause the state of
// the development database and production database to slowly get out of sync.
//
// If you have a style change like making all SQL keywords uppercase you can use
// RepairHash to rehash the migration and update the Hash in the database.
//
// If you want to skip the hash validation you can use RunUnsafe instead.
func (m *Migrator) Run() ([]MigrationLog, error) {
	m.safe = true
	err := m.run()
	return m.log, err
}

// RunUnsafe executes the new migrations against the DB like Run, but in unsafe
// mode. Unsafe mode will not stop and return an error if an existing record
// hash does not match the hash of the migration.
//
// The reason you may not want the hash checked on each subsequent run is simple.
// "alter table" and "ALTER TABLE" produce the same results, but have a
// different hash. To keep auto-formatters and linter changes from breaking old
// migrations RunUnsafe will ignore these and all other changes to the statement
// and args.
func (m *Migrator) RunUnsafe() ([]MigrationLog, error) {
	m.safe = false
	err := m.run()
	return m.log, err
}

// run all the Migrator.migrations.
func (m *Migrator) run() error {
	// Create the migration table if it does not exist
	exists, err := m.backend.HasMigrationTable()
	if err != nil {
		return fmt.Errorf("the migration table check failed: %w", err)
	}
	if !exists {
		err := m.createMigrationTable()
		if err != nil {
			return fmt.Errorf("create '%s' table failed: %w", m.TableName, err)
		}
	}

	// Create transaction for migrations
	tx, err := m.db.Beginx()
	if err != nil {
		return fmt.Errorf("begin transaction failed: %w", err)
	}
	commit := true
	defer func() {
		if commit {
			tx.Commit()
			return
		}
		tx.Rollback()
	}()

	err = m.repairHashes(tx)
	if err != nil {
		commit = false
		return fmt.Errorf("repair hashes failed: %w", err)
	}

	// Get previous migrations
	prev, err := m.backend.QueryPrevious()
	if err != nil {
		commit = false
		return fmt.Errorf("get previous migrations failed: %w", err)
	}
	m.previous = prev

	// Run each migration
	for _, mig := range m.migrations {
		err = m.executeMigration(tx, mig)
		if err != nil {
			commit = false
			return fmt.Errorf("run error on '%s': %w", mig.Name, err)
		}
	}
	return err
}

// Executes a single migration
func (m *Migrator) executeMigration(tx *sqlx.Tx, mig Migration) error {
	mLog := MigrationLog{
		Name:    mig.Name,
		Hash:    mig.hash,
		Status:  SUCCESS,
		Details: "ran migration successfully",
	}
	defer func() {
		m.log = append(m.log, mLog)
	}()

	_, exists := m.previous[mig.Name]
	if exists {
		mLog.Status = PREVIOUS
		mLog.Details = "migration already run"
		h, valid := m.hashIsValid(mig)
		if !valid {
			d := fmt.Sprintf("hash mismatch DB: '%s' Migration: '%s'", h, mig.hash)
			mLog.Details = d
			if m.safe {
				mLog.Status = ERROR_HASH
				return fmt.Errorf("%s %s", mig.Name, d)
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

// Gets the new hashes calls the backend RepairHashes method.
func (m *Migrator) repairHashes(tx *sqlx.Tx) error {
	if len(m.repair) == 0 {
		return nil
	}
	for _, mig := range m.migrations {
		if _, ok := m.repair[mig.Name]; !ok {
			continue
		}
		m.repair[mig.Name] = mig.hash
	}

	return m.backend.RepairHashes(tx, m.repair)
}

// hashIsValid returns stored hash and true if the hash is valid.
func (m Migrator) hashIsValid(mig Migration) (string, bool) {
	repaired, exists := m.repair[mig.Name]
	if exists && repaired == mig.hash {
		return repaired, true
	}
	previous, exists := m.previous[mig.Name]
	if !exists {
		return mig.hash, true
	}
	return previous, previous == mig.hash
}

// New creates and returns a new Migrator instance. You typically should use one
// Migrator per database.
func New(db *sqlx.DB, tableName string, tableSchema string) (Migrator, error) {
	m := Migrator{
		db:          db,
		TableName:   tableName,
		tableSchema: tableSchema,
		previous:    make(map[string]string),
		migrations:  make([]Migration, 0, 1),
		repair:      make(map[string]string),
		names:       make(map[string]struct{}),
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
