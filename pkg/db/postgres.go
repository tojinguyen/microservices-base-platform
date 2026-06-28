package db

import (
	"backend/pkg/logger"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func openPostgres(dsn string) (*gorm.DB, error) {
	return gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: NewZapGormLogger(logger.L()),
	})
}

