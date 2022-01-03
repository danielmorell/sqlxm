# sqlx migrator (sqlxm)

sqlxm runs a set of migrations against a DB. It is designed to work with the popular 
[sqlx](https://github.com/jmoiron/sqlx) package by Jason Moiron.

## Features

**Idempotent** One of the critical things required in DB schema changes is ensuring the changes are made once. sqlxm
will run each migration once.

**Data Migrations** Because it is common for schema changes to also require data changes, sqlxm will let you run any SQL
query you want. You read that right. If you can write a SQL query sqlxm can run it.

**Migration Integrity** The accidental editing of SQL migration statements can introduce subtle bugs into an 
application. To prevent this sqlxm validates the integrity of every previously run query against its current 
version using a checksum.

## Best Practices

**Run on startup.** For most applications the best time to run your DB migrations is on start up.

**Don't remove old migrations.** As your application ages and changes the number of migrations will grow. Since they 
only need to be run once on your production database it may be tempting to prune some of them. However, it is 
recommended that you keep them. This makes creating new development, testing, or staging environments reproducible,
deterministic and consistent with production. 

## Installation

It is not too complicated.

```
$ go get github.com/danielmorell/sqlxm
```

## Basic Usage

There are three basic functions that set up sqlx migrator and start running your migrations.

1. `sqlxm.New()` creates a new `sqlxm.Migrator` instance that can be used to track and run migrations.
2. `Migrator.AddMigration()` creates a new migration to run and keep track of. Migrations are run in the order they are
   added.
3. `Migrator.Run()` takes all the previous migrations added with `Migrator.AddMigration()` and makes sure they have been
   applied to database or applies them.

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
	xm, err := sqlxm.New(db, "migrations", "public")
	if err != nil {
		log.Fatalln(err)
	}

	// Add a few migrations
	xm.AddMigration(
		// A human-readable key for the migration
		"create_user_table",
		// A comment to explain what the migration does
		"Add the initial user table",
		// The migration SQL statement
		`CREATE TABLE users (
        	id         SERIAL 
                CONSTRAINT users_pk PRIMARY KEY,
			username   VARCHAR(64)  NOT NULL,
			email      VARCHAR(64)  NOT NULL,
			password   VARCHAR(128) NOT NULL);
		
		CREATE UNIQUE INDEX users_name_uindex ON users (username);`,
	)

	xm.AddMigration(
		"create_posts_table",
		"Add the initial blog posts table",
		`CREATE TABLE posts (
        	id     SERIAL 
                CONSTRAINT posts_pk PRIMARY KEY,
			slug   VARCHAR(128)               NOT NULL,
			body   TEXT                       NOT NULL,
			date   TIMESTAMP    DEFAULT NOW() NOT NULL,
            author INT
                CONSTRAINT posts_users_id_fk
                    REFERENCES users
                    ON UPDATE CASCADE ON DELETE RESTRICT);
	
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

## Advanced Usage / Design

### Migration Hashing

sqlxm creates a hash of the migration statement and arguments. This ensures that any change to the migration query
itself or any arguments will not go unnoticed. This hash is used as a checksum to validate previously run migrations.
This keeps the code you used to run past migrations and the current state of your database from getting out of sync.

You may be wondering what the rational is for doing this. It would be too easy to accidentally, change a column from
nullable to `NOT NULL` in an old migration. If a new instance of the DB was created it would have the column as not
nullable, however, a production DB may allow `NULL` this can introduce bugs into your codebase as production may be
returning `NULL` when it is not expected.

### Safe Mode

For the most part it is recommended that you run migrations in **safe mode**. You do this by simply calling the
`Migrator.Run()` method. It works a bit like a compile error in Go. If there is a potential to create an error or
unknown state sqlxm will stop the migration.

sqlxm does this by checking the hash stored in the database with the hash of the migration. If the hash check fails, the
migration stops and a hash mismatch error is returned.

If you want to run the migrations in **unsafe mode**, you can do so by calling `Migrator.RunUnsafe()`.

### Hash Repair

There are times when non-substantive changes (like indentation) may be made to a migration query. *For the most part,
changing migration queries is a bad idea and should be avoided.* But it is recognized that `alter table`
and `ALTER TABLE` do the same thing but produce a different hash.

In a scenario where you need to update the hash of the migration, you can use the `Migrator.RepairHash()` method to
update the hash of previous migrations.

**Note:** safe mode will not prevent you from writing `DROP TABLE users` as a migration. It simply validates the
integrity of the migration source with the already run migration.

### Backends

**Pre-built backends**

- MySQL - key: `mysql`
- Postgres - key: `postgres`
- SQLite - key: `sqlite`

You can easily write your own backend by implementing the `Backend` interface from the `sqlxm/backends` package.

You will need to register your custom backend by calling the `RegisterBackend()` function. Then you can tell your
`Migrator` instance to use that backend by calling the `Migrator.UseBackend()` method and passing in the key for the
backend that you registered.

Note: you cannot overwrite an existing backend. However, you can simply specify a new key.

If you use one of the common database drivers for a DBMS with a pre-build backend, sqlxm should automatically know 
what backend to use. This helps reduce the boilerplate needed to run migrations. However, if you are using a special 
database driver you can always call `Migrator.UseBackend()` to specify the backend you want to use.

## Testing

Because a database connection is required to run tests, I recommend using Docker to run the DB engines.

### Setup Docker

**1. Create `.env` File**

Copy the `.sample.env` file to `.env` and make any needed changes to the values. This `.env` file will be read by both
Docker and the sqlxm tests.

**2. Start Docker Containers**

```
$ docker compose up -d
```

**3. Run the Tests**

```
$ go test -v
```