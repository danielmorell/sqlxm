package main

import (
	"fmt"
	"log"

	"github.com/danielmorell/sqlxm"
	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {
	pg, done := connectTestDB()
	defer done()

	migrator := sqlxm.New(pg, "migrations")

	migrator.AddMigration(
		"create_user_table",
		"Add the initial user table",
		// language=PostgreSQL
		`CREATE TABLE users
        (
        	id         SERIAL       NOT NULL,
			username   VARCHAR(64)  NOT NULL,
			email      VARCHAR(64)  NOT NULL,
			first_name VARCHAR(64)  NOT NULL,
			last_name  VARCHAR(64)  NOT NULL,
			password   VARCHAR(128) NOT NULL
        );

		ALTER TABLE users
			ADD CONSTRAINT users_pk PRIMARY KEY (id);
	
		CREATE UNIQUE INDEX users_id_uindex ON users (id);
		
		CREATE UNIQUE INDEX users_name_uindex ON users (username);`,
	)

	mLog, err := migrator.Run()
	if err != nil {
		log.Fatal(err)
	}
	for _, l := range mLog {
		stat := "success"
		if l.Error != nil {
			stat = fmt.Sprintf("fail - %s", err)
		}
		log.Printf("%s :: %s STATUS %s", l.Hash, l.Name, stat)
	}
}

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
