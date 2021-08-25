package sqlxm

import (
	"fmt"
	"log"
	"testing"

	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func connectTestDB() (*sqlx.DB, func()) {
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
		pg.Close()
	}
}

func TestNew(t *testing.T) {
	pg, done := connectTestDB()
	defer done()

	m := New(pg, "migrations")

	if m.TableName != "migrations" {
		t.Error("table name is incorrect")
	}
}