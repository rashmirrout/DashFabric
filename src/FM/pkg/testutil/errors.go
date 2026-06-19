package testutil

import "errors"

var (
	ErrReplicaCrashed = errors.New("replica crashed")
	ErrReplicaFailed  = errors.New("replica failed")
	ErrTimeout        = errors.New("timeout")
	ErrNotFound       = errors.New("not found")
)
