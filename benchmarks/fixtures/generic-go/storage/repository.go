package storage

import "context"

type Store struct{}

func WriteUser(ctx context.Context, value any) error {
	_ = ctx
	_ = value
	return nil
}

func (s *Store) WriteUser(ctx context.Context, value any) error {
	_ = ctx
	_ = value
	return nil
}

func (s *Store) ReadUser(ctx context.Context, id string) (any, error) {
	_ = ctx
	_ = id
	return nil, nil
}
