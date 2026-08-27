package services

import (
	"context"
	"errors"
	"fmt"
)

var ErrPermissionDenied = errors.New("permission denied")

type PermissionChecker interface {
	UserHasPermission(ctx context.Context, userID, businessID, permission string) bool
}

func requireMutationPermission(ctx context.Context, checker PermissionChecker, businessID, permission string) error {
	actor := ActorFromContext(ctx)
	if checker == nil || actor.UserID == "" || businessID == "" || !checker.UserHasPermission(ctx, actor.UserID, businessID, permission) {
		return fmt.Errorf("%w: %s", ErrPermissionDenied, permission)
	}
	return nil
}
