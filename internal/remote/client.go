// Package remote provides access to files and commands on a managed host.
package remote

import (
	"context"
	"errors"
)

// ErrNotFound indicates that a requested remote path does not exist.
var ErrNotFound = errors.New("remote path not found")

// Client abstracts the remote operations used by resources.
type Client interface {
	ReadFile(context.Context, string) ([]byte, error)
	WriteFile(context.Context, string, []byte) error
	RemoveFile(context.Context, string) error
	Run(context.Context, string) (string, error)
}
