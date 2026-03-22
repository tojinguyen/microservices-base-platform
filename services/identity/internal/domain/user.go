package domain

import "golang.org/x/crypto/bcrypt"

type User struct {
	BaseModel
	Email        string `gorm:"uniqueIndex;not null;size:255" json:"email"`
	Role         string `gorm:"size:50;default:'user'" json:"role"`
	PasswordHash string `gorm:"not null" json:"-"`
	Name         string `gorm:"size:255" json:"name"`
}

func (u *User) HashPassword(password string) error {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	u.PasswordHash = string(bytes)
	return nil
}

func (u *User) CheckPassword(password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password))
	return err == nil
}
