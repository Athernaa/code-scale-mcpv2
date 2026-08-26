package httpapi

import (
	"context"

	"bench/generic-go/user"
)

func HandleSave(ctx context.Context, value user.User) error {
	return user.SaveUser(ctx, value)
}

func HandleHealth() string { return "ok" }
