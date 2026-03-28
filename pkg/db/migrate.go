package db

import (
	"database/sql"
	"embed"

	"github.com/pressly/goose/v3"
)

// RunMigrations applies goose migrations to the database.
// It accepts the standard *sql.DB and the embed.FS from the calling service.
func RunMigrations(db *sql.DB, fsys embed.FS, dir string) error {
	goose.SetBaseFS(fsys)

	if err := goose.SetDialect("postgres"); err != nil {
		return err
	}

	return goose.Up(db, dir)
}
