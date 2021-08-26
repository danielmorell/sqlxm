# SQLX Migrator

SQLXM runs a set of migrations against a DB.

## Features

**Idempotent** One of the critical things required in DB schema changes is ensuring the changes are made once. SQLXM
will run each migration once.

**Data Migrations** Because it is common for schema changes to also require data changes, SQLXM will let you 
run any SQL query you want. You read that right. If you can write a SQL query SQLXM can run it.

## Best Practices

**Run on startup.** For most applications the best time to run your DB migrations is on start up.

## Installation 

It is not too complicated.

```
$ go get github.com/danielmorell/sqlxm
```

## Basic Usage

There are three basic functions that set up SQLX Migrator and start running your migrations. 

1. `sqlxm.New()` creates a new `sqlxm.Migrator` instance that can be used to track and run migrations.
2. `xm.AddMigration()` creates a new migration to run and keep track of.
3. `xm.Run()` takes all the previous migrations added with `xm.AddMigration()` and makes sure they have been applied 
   to database or applies them.

```go
package main

import (
	"log"

	"github.com/danielmorell/sqlxm"
	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

func main() {
	// Create DB connection
	db, err := sqlx.Open("postgres", "user=me dbname=db sslmode=disable")
	if err != nil {
		log.Fatalln(err)
	}
	defer db.Close()

	// Create new migrator
	xm := sqlxm.New(db, "migrations")

	// Add a few migrations
	xm.AddMigration(
		"create_user_table",
		"Add the initial user table",
		`CREATE TABLE users
        (
        	id         SERIAL
                CONSTRAINT users_pk 
                    PRIMARY KEY,
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

	xm.AddMigration(
		"create_posts_table",
		"Add the initial blog posts table",
		`CREATE TABLE posts
        (
        	id     SERIAL
                CONSTRAINT posts_pk 
                    PRIMARY KEY,
			slug   VARCHAR(128)               NOT NULL,
			body   TEXT                       NOT NULL,
			date   TIMESTAMP    DEFAULT NOW() NOT NULL,
            author INT
                CONSTRAINT posts_users_id_fk
                    REFERENCES users
                        ON UPDATE CASCADE ON DELETE RESTRICT     
        );
	
		CREATE UNIQUE INDEX posts_slug_uindex ON posts (slug);`,
	)

	// Run the migrator
	migrationLog, err := xm.Run()
	if err != nil {
		log.Fatalln(err)
	}

	// Log the results
	for _, l := range migrationLog {
		log.Printf("%v", l)
	}
}
```

## Testing

Because a database connection is required to run tests, I recommend using Docker to run the DB engines.

### Setup Docker

**1. Create `.env` File**

Copy the `.sample.env` file to `.env` and make any needed changes to the values. This `.env` file will be read by both
Docker and the SQLXM tests.

**2. Start Docker Containers**

```
$ docker compose up -d
```