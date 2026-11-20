// Package logging builds the JSON logger every service uses.
package logging

import (
	"log/slog"
	"os"
)

// New returns a JSON logger to stdout, tagged with the service name.
func New(service string) *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("service", service)
}
