package repository

import (
	"context"

	"github.com/tojinguyen/identity/internal/domain"
	"gorm.io/gorm"
)

//go:generate mockery --name=UserRepository

type UserRepository interface {
	Create(ctx context.Context, user *domain.User) error
	BulkCreate(ctx context.Context, users []*domain.User, batchSize int) error
	GetByEmail(ctx context.Context, email string) (*domain.User, error)
	GetByID(ctx context.Context, id string) (*domain.User, error)
	Update(ctx context.Context, user *domain.User) error
	Delete(ctx context.Context, id string) error

	// ListUsers returns up to limit users whose ID is strictly greater than cursor,
	// ordered by id ASC. Pass cursor="" to start from the beginning. Pass role="" to include all roles.
	ListUsers(ctx context.Context, role string, cursor string, limit int) ([]*domain.User, error)
	// CountUsers returns the total number of users matching the optional role filter.
	CountUsers(ctx context.Context, role string) (int64, error)
}

type userRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) UserRepository {
	return &userRepository{db: db}
}

func (r *userRepository) Create(ctx context.Context, user *domain.User) error {
	return r.db.WithContext(ctx).Create(user).Error
}

func (r *userRepository) BulkCreate(ctx context.Context, users []*domain.User, batchSize int) error {
	return r.db.WithContext(ctx).CreateInBatches(users, batchSize).Error
}

func (r *userRepository) GetByEmail(ctx context.Context, email string) (*domain.User, error) {
	var user domain.User
	err := r.db.WithContext(ctx).Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) GetByID(ctx context.Context, id string) (*domain.User, error) {
	var user domain.User
	err := r.db.WithContext(ctx).Where("id = ?", id).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *userRepository) Update(ctx context.Context, user *domain.User) error {
	return r.db.WithContext(ctx).Save(user).Error
}

func (r *userRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&domain.User{}).Error
}

func (r *userRepository) ListUsers(ctx context.Context, role string, cursor string, limit int) ([]*domain.User, error) {
	var users []*domain.User
	q := r.db.WithContext(ctx).Where("deleted_at IS NULL")
	if role != "" {
		q = q.Where("role = ?", role)
	}
	if cursor != "" {
		q = q.Where("id > ?", cursor)
	}
	err := q.Order("id ASC").Limit(limit).Find(&users).Error
	return users, err
}

func (r *userRepository) CountUsers(ctx context.Context, role string) (int64, error) {
	var count int64
	q := r.db.WithContext(ctx).Model(&domain.User{}).Where("deleted_at IS NULL")
	if role != "" {
		q = q.Where("role = ?", role)
	}
	err := q.Count(&count).Error
	return count, err
}
