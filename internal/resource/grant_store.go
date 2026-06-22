package resource

import (
	"context"
	"errors"
	"time"
)

var (
	ErrGrantNotFound     = errors.New("resource grant not found")
	ErrChangeSetNotFound = errors.New("host resource changeset not found")
)

type GrantStore interface {
	Create(context.Context, GrantEnvelope) (GrantEnvelope, error)
	Get(context.Context, string) (GrantEnvelope, bool, error)
	List(context.Context, GrantListFilter) ([]GrantEnvelope, error)
	Consume(context.Context, string, PrincipalRef) (GrantEnvelope, error)
	Revoke(context.Context, string, PrincipalRef) (GrantEnvelope, error)
	Expire(context.Context, time.Time) (int, error)
}
