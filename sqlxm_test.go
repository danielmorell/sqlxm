package sqlxm

import (
	"fmt"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/danielmorell/sqlxm/backends"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

// Test Types / Fixtures -------------------------------------------------------

// backends.Backend implementation
type back struct {
	db          *sqlx.DB
	table       string
	tableSchema string
}

func (b *back) Setup(db *sqlx.DB, table string, tableSchema string) {
}

func (b *back) InsertRecord(tx *sqlx.Tx, name string, hash string, comment string) error {
	return nil
}

func (b *back) HasMigrationTable() (bool, error) {
	return false, nil
}

func (b *back) QueryPrevious() (map[string]string, error) {
	return make(map[string]string), nil
}

func (b *back) CreateMigrationTable() (string, error) {
	return "", nil
}

func (b *back) RepairHashes(tx *sqlx.Tx, hashes map[string]string) error {
	return nil
}

type testDBMS struct {
	title       string
	name        string
	tableSchema string
}

// Unit Tests ------------------------------------------------------------------

func TestBackendType(t *testing.T) {
	// 0: backend, 1: driver
	drivers := [][2]string{
		{"postgres", "postgres"},
		{"postgres", "cockroach"},
		{"mysql", "mysql"},
		{"sqlite", "sqlite3"},
		{"oracle", "godror"},
		{"sqlserver", "sqlserver"},
	}

	t.Run("KnownBackends", func(t *testing.T) {
		for i, d := range drivers {
			backend := BackendType(d[1])
			if backend != d[0] {
				t.Errorf("backend type #%d incorrect expected '%s', actuall '%s'", i, d[0], backend)
			}
		}
	})
	t.Run("UnknownBackend", func(t *testing.T) {
		backend := BackendType("not-a-real-driver")
		if backend != "unknown" {
			t.Error("backend should return 'unknown'")
		}
	})
}

func TestRegisterBackend(t *testing.T) {
	t.Run("ExistingBackend", func(t *testing.T) {
		err := RegisterBackend("postgres", &back{})
		if err == nil {
			t.Error("backend exists: an error should be returned")
		}
	})
	t.Run("NewBackend", func(t *testing.T) {
		err := RegisterBackend("mydb", &back{})
		if err != nil {
			t.Error("backend does not exist: an error should not be returned")
		}
	})
}

// End-to-End Tests ------------------------------------------------------------

func dropTables(db *sqlx.DB, tables []string) {
	for _, t := range tables {
		db.Exec(fmt.Sprintf("DROP TABLE %s;", t))
	}
}

func getEnv() map[string]string {
	var env map[string]string
	env, err := godotenv.Read()
	if err != nil {
		panic(err)
	}
	return env
}

func postgresDSN(env map[string]string) string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		env["POSTGRES_HOST"],
		env["POSTGRES_PORT"],
		env["POSTGRES_USER"],
		env["POSTGRES_PASSWORD"],
		env["POSTGRES_DB"],
	)
}

func mysqlDSN(env map[string]string) string {
	// username:password@protocol(address)/dbname?param=value
	return fmt.Sprintf(
		"%s:%s@(%s:%s)/%s",
		env["MYSQL_USER"],
		env["MYSQL_PASSWORD"],
		env["MYSQL_HOST"],
		env["MYSQL_PORT"],
		env["MYSQL_DB"],
	)
}

func connectToDB(dbms string) (*sqlx.DB, func(drop ...string)) {
	env := getEnv()
	var sourceData string
	switch dbms {
	case "mysql":
		sourceData = mysqlDSN(env)
	case "postgres":
		sourceData = postgresDSN(env)
	case "sqlite":
		sourceData = env["SQLITE_PATH"]
	}

	db, err := sqlx.Open(dbms, sourceData)
	if err != nil {
		log.Fatalf("DB connection %s: %s\n", dbms, err)
	}

	return db, func(drop ...string) {
		dropTables(db, drop)
		db.Close()

		if dbms == "sqlite" {
			os.Remove(env["SQLITE_PATH"])
		}
	}
}

func TestMainE2E(t *testing.T) {
	env := getEnv()
	dbms := []testDBMS{
		{
			title:       "MySQL",
			name:        "mysql",
			tableSchema: env["MYSQL_DB"],
		},
		{
			title:       "Postgres",
			name:        "postgres",
			tableSchema: "public",
		},
		{
			title:       "SQLite",
			name:        "sqlite",
			tableSchema: "",
		},
	}

	for _, d := range dbms {
		t.Run(fmt.Sprintf("%sTestE2E", d.title), func(t *testing.T) {
			testE2E(t, d)
		})
		t.Run(fmt.Sprintf("%stestStrict", d.title), func(t *testing.T) {
			testStrict(t, d)
		})
		t.Run(fmt.Sprintf("%stestDuplicate", d.title), func(t *testing.T) {
			testDuplicate(t, d)
		})
		t.Run(fmt.Sprintf("%stestUseBackend", d.title), func(t *testing.T) {
			testUseBackend(t, d)
		})
	}
}

func testE2E(t *testing.T, dbms testDBMS) {
	db, done := connectToDB(dbms.name)
	defer done("migrations", "users")

	migrator, err := New(db, "migrations", dbms.tableSchema)
	if err != nil {
		t.Error(err)
	}

	err = migrator.AddMigration(
		"create_user_table",
		"Add the initial user table",
		// language=SQL
		`CREATE TABLE users (
            id       INT,
        	name     VARCHAR(64) NOT NULL,
			password VARCHAR(128) NOT NULL
        );`,
	)
	if err != nil {
		t.Error(err)
	}

	err = migrator.AddMigration(
		"add_user_username_column",
		"Add the user username column",
		// language=SQL
		`ALTER TABLE users ADD username VARCHAR(64) NOT NULL;`,
	)
	if err != nil {
		t.Error(err)
	}

	err = migrator.AddMigration(
		"add_user_username_uindex",
		"Unique index the users table username column",
		// language=SQL
		`CREATE UNIQUE INDEX users_username_uindex ON users (username);`,
	)
	if err != nil {
		t.Error(err)
	}

	l, err := migrator.Run()
	if err != nil {
		t.Errorf("migrator run error: %s", err)
	}

	t.Run("MigrationLogCount", func(t *testing.T) {
		if len(l) != 4 {
			t.Errorf("migration log count incorrect: expcted '4', got '%d'", len(l))
		}
	})

	// Make sure user table exists.
	t.Run("UserTableCreated", func(t *testing.T) {
		exists := false
		if dbms.name == "sqlite" {
			err = db.Get(&exists, `SELECT count(name)
			FROM sqlite_master 
			WHERE type='table' 
			AND name = 'users';`)
		} else {
			err = db.Get(&exists, fmt.Sprintf(`SELECT EXISTS(
			SELECT * FROM information_schema.tables
			WHERE table_schema = '%s' 
			  AND table_name = 'migrations'
			);`, dbms.tableSchema))
		}
		if err != nil {
			t.Error("error querying user table")
		}

		if !exists {
			t.Error("'users' table was not created.")
		}
	})

	// Make sure user table has the right columns
	t.Run("UserTableColumnsCorrect", func(t *testing.T) {
		cols := make([]string, 0, 4)
		if dbms.name == "sqlite" {
			err = db.Select(&cols, `SELECT name as column_name FROM pragma_table_info(('users'));`)
		} else {
			err = db.Select(&cols, fmt.Sprintf(`SELECT column_name
			FROM information_schema.columns
			WHERE table_schema = '%s'
				AND table_name = 'users';`, dbms.tableSchema))
		}
		if err != nil {
			t.Error("error querying user table columns")
		}
		expectedCols := map[string]bool{
			"id":       false,
			"name":     false,
			"password": false,
			"username": false,
		}
		for _, col := range cols {
			_, exists := expectedCols[col]
			if !exists {
				t.Errorf("unexpected column '%s'", col)
				continue
			}
			expectedCols[col] = true
		}
		for col, exists := range expectedCols {
			if !exists {
				t.Errorf("column not created '%s'", col)
			}
		}
	})
}

// Test to make sure that migrations with the same name will
func testStrict(t *testing.T, dbms testDBMS) {
	pg, done := connectToDB(dbms.name)
	defer done("migrations", "users")

	migrator1, err := New(pg, "migrations", dbms.tableSchema)
	if err != nil {
		t.Error(err)
	}

	err = migrator1.AddMigration(
		"create_user_table",
		"Add the initial user table",
		// language=PostgreSQL
		`CREATE TABLE users (
            id       INT,
        	name     VARCHAR(64) NOT NULL,
			password VARCHAR(128) NOT NULL
        );`,
	)
	if err != nil {
		t.Error(err)
	}
	_, err = migrator1.Run()
	if err != nil {
		t.Errorf("migrator run error: %s", err)
	}

	// In strict mode the second run will return an error.
	t.Run("RunStrict", func(t *testing.T) {
		migrator2, err := New(pg, "migrations", dbms.tableSchema)
		if err != nil {
			t.Error(err)
		}
		err = migrator2.AddMigration(
			"create_user_table",
			"Add the initial user table",
			// language=PostgreSQL
			`CREATE TABLE users (
				id INT,
				name VARCHAR(64) NOT NULL,
				password VARCHAR(128) NOT NULL
			);`,
		)
		if err != nil {
			t.Error(err)
		}

		l, err := migrator2.RunStrict()
		if err == nil || ERROR_HASH != l[len(l)-1].Status {
			t.Error("migrator run strict error: hash mismatch check failed")
		}
	})

	t.Run("RunLoose", func(t *testing.T) {
		migrator3, err := New(pg, "migrations", dbms.tableSchema)
		if err != nil {
			t.Error(err)
		}
		err = migrator3.AddMigration(
			"create_user_table",
			"Add the initial user table",
			// language=PostgreSQL
			`CREATE TABLE users (
				id INT,
				name VARCHAR(64) NOT NULL,
				password VARCHAR(128) NOT NULL
			);`,
		)
		if err != nil {
			t.Error(err)
		}

		l, err := migrator3.Run()
		lastLog := l[len(l)-1]
		if err != nil {
			t.Error("migrator run loose error: run failed")
		}
		if PREVIOUS != lastLog.Status {
			t.Error("migrator run loose error: previous status not set")
		}
		if !strings.Contains(lastLog.Details, "hash mismatch") {
			t.Error("migrator run loose error: hash message missing")
		}
	})

	t.Run("RepairHash", func(t *testing.T) {
		migrator4, err := New(pg, "migrations", dbms.tableSchema)
		if err != nil {
			t.Error(err)
		}
		err = migrator4.AddMigration(
			"create_user_table",
			"Add the initial user table",
			// language=PostgreSQL
			`CREATE TABLE users (
				id INT,
				name VARCHAR(64) NOT NULL,
				password VARCHAR(128) NOT NULL
			);`,
		)
		if err != nil {
			t.Error(err)
		}

		migrator4.RepairHash("create_user_table")

		l, err := migrator4.RunStrict()

		var lastLog = MigrationLog{}
		if len(l) > 0 {
			lastLog = l[len(l)-1]
		}

		if err != nil {
			t.Error("migrator run hash repair error: run failed")
		}
		if PREVIOUS != lastLog.Status {
			t.Error("migrator run hash repair error: previous status not set")
		}
	})
}

// Test to make sure that migrations with the same name will fail
func testDuplicate(t *testing.T, dbms testDBMS) {
	pg, done := connectToDB(dbms.name)
	defer done()

	migrator, err := New(pg, "migrations", dbms.tableSchema)
	if err != nil {
		t.Error(err)
	}

	err = migrator.AddMigration("foo", "Foo", "")
	if err != nil {
		t.Errorf("add migration failed: %s", err)
	}

	err = migrator.AddMigration("bar", "Bar", "")
	if err != nil {
		t.Errorf("add migration failed: %s", err)
	}

	// This should return an error since the name already exists.
	err = migrator.AddMigration("foo", "Foo", "")
	if err == nil {
		t.Errorf("add migration succeeded: %s", err)
	}
}

func testUseBackend(t *testing.T, dbms testDBMS) {
	pg, done := connectToDB(dbms.name)
	defer done("migrations", "users")

	// For simplicity, we will just register the Postgres backend under a
	// different name.
	err := RegisterBackend("mydb", &backends.Postgres{})

	migrator, err := New(pg, "migrations", dbms.tableSchema)
	if err != nil {
		t.Error(err)
	}

	t.Run("UnknownBackend", func(t *testing.T) {
		err = migrator.UseBackend("nope")
		if err == nil {
			t.Error("backend should not exist")
		}
	})

	t.Run("KnownBackend", func(t *testing.T) {
		err = migrator.UseBackend("mydb")
		if err != nil {
			t.Error(err)
		}

		err = migrator.AddMigration(
			"create_user_table",
			"Add the initial user table",
			// language=PostgreSQL
			`CREATE TABLE users (
            id       INT,
        	name     VARCHAR(64) NOT NULL,
			password VARCHAR(128) NOT NULL
        );`,
		)
		if err != nil {
			t.Error(err)
		}
		_, err = migrator.Run()
		if err != nil {
			t.Errorf("migrator run error: %s", err)
		}

	})
}
