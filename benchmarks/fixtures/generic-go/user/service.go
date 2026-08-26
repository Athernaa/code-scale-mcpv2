package user

import (
	"context"

	"bench/generic-go/storage"
)

func ValidateUser(value User) error {
	if value.ID == "" || value.Email == "" {
		return errInvalidUser{}
	}
	return nil
}

func SaveUser(ctx context.Context, value User) error {
	if err := ValidateUser(value); err != nil {
		return err
	}
	return storage.WriteUser(ctx, value)
}

type errInvalidUser struct{}

func (errInvalidUser) Error() string { return "invalid user" }
