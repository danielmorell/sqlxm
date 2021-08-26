package sqlxm

import (
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func connectPostgresDB(tableName string) (*sqlx.DB, func()) {
	var env map[string]string
	env, err := godotenv.Read()
	if err != nil {
		panic(err)
	}

	pg, err := sqlx.Open("postgres", fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
		env["POSTGRES_HOST"],
		env["POSTGRES_PORT"],
		env["POSTGRES_USER"],
		env["POSTGRES_PASSWORD"],
		env["POSTGRES_DB"],
	))
	if err != nil {
		log.Fatalf("DB connection: %s", err)
	}

	return pg, func() {
		pg.Exec(fmt.Sprintf(`DROP TABLE %s;`, tableName))
		pg.Close()
	}
}

func TestPostgresE2E(t *testing.T) {
	pg, done := connectPostgresDB("migrations")
	defer done()

	migrator, err := New(pg, "migrations")
	if err != nil {
		log.Fatal(err)
	}

	err = migrator.AddMigration(
		"create_user_table",
		"Add the initial user table",
		// language=PostgreSQL
		`CREATE TABLE users
        (
            id       SERIAL CONSTRAINT users_pk PRIMARY KEY,
        	name     VARCHAR(64) NOT NULL,
			password VARCHAR(128) NOT NULL
        );`,
	)
	if err != nil {
		log.Fatal(err)
	}

	err = migrator.AddMigration(
		"add_user_username_column",
		"Add the user username column",
		// language=PostgreSQL
		`ALTER TABLE users ADD username VARCHAR(64) NOT NULL;
		CREATE UNIQUE INDEX users_username_uindex ON users (username);`,
	)
	if err != nil {
		log.Fatal(err)
	}

	l, err := migrator.Run()
	if err != nil {
		t.Errorf("migrator run error: %s", err)
	}

	t.Run("MigrationLogCount", func(t *testing.T) {
		if len(l) != 3 {
			t.Errorf("migration log count incorrect")
		}
	})

	// Make sure user table exists.
	t.Run("UserTableCreated", func(t *testing.T) {
		exists := false
		err = pg.Get(&exists, `SELECT EXISTS(
		SELECT *
		FROM information_schema.tables
		WHERE
		  table_schema = 'public' AND
          table_name = 'migrations'
		);`)
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
		err = pg.Select(&cols, `SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
			AND table_name = 'users';`)
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

	pg.Exec(`DROP TABLE users;`)
}

// Test to make sure that migrations with the same name will
func TestPostgresStrict(t *testing.T) {
	pg, done := connectPostgresDB("migrations")
	defer done()

	migrator1, err := New(pg, "migrations")
	if err != nil {
		log.Fatal(err)
	}

	err = migrator1.AddMigration(
		"create_user_table",
		"Add the initial user table",
		// language=PostgreSQL
		`CREATE TABLE users
        (
            id       SERIAL CONSTRAINT users_pk PRIMARY KEY,
        	name     VARCHAR(64) NOT NULL,
			password VARCHAR(128) NOT NULL
        );`,
	)
	if err != nil {
		log.Fatal(err)
	}
	_, err = migrator1.Run()
	if err != nil {
		t.Errorf("migrator run error: %s", err)
	}

	// In strict mode the second run will return an error.
	t.Run("RunStrict", func(t *testing.T) {
		migrator2, err := New(pg, "migrations")
		if err != nil {
			log.Fatal(err)
		}
		err = migrator2.AddMigration(
			"create_user_table",
			"Add the initial user table",
			// language=PostgreSQL
			`CREATE TABLE users
        (
            id SERIAL CONSTRAINT users_pk PRIMARY KEY,
        	name VARCHAR(64) NOT NULL,
			password VARCHAR(128) NOT NULL
        );`,
		)
		if err != nil {
			log.Fatal(err)
		}

		l, err := migrator2.RunStrict()
		if err == nil || ERROR_HASH != l[len(l)-1].Status {
			t.Error("migrator run strict error: hash mismatch check failed")
		}
	})

	t.Run("RunLoose", func(t *testing.T) {
		migrator3, err := New(pg, "migrations")
		if err != nil {
			log.Fatal(err)
		}
		err = migrator3.AddMigration(
			"create_user_table",
			"Add the initial user table",
			// language=PostgreSQL
			`CREATE TABLE users
        (
            id SERIAL CONSTRAINT users_pk PRIMARY KEY,
        	name VARCHAR(64) NOT NULL,
			password VARCHAR(128) NOT NULL
        );`,
		)
		if err != nil {
			log.Fatal(err)
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

	pg.Exec(`DROP TABLE users;`)
}

// Test to make sure that migrations with the same name will fail
func TestPostgresDuplicate(t *testing.T) {
	pg, done := connectPostgresDB("migrations")
	defer done()

	migrator, err := New(pg, "migrations")
	if err != nil {
		log.Fatal(err)
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
