package db

import "gorm.io/gorm"

func WithTx(db *gorm.DB, fn func(tx *gorm.DB) error) error {
	tx := db.Begin()

	if tx.Error != nil {
		return tx.Error
	}

	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit().Error
}
