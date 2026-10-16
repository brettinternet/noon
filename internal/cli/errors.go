package cli

import "github.com/brettinternet/noon/internal/app"

// Error describes a CLI failure independently of its output format.
type Error = app.Error

func usageError(message, hint string) *Error { return app.UsageError(message, hint) }

func configError(err error) error {
	if err == nil {
		return nil
	}
	return app.ConfigError(err)
}
