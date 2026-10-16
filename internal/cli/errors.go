package cli

// Error describes a CLI failure independently of its output format.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Usage   bool   `json:"-"`
}

func (e *Error) Error() string { return e.Message }

func usageError(message, hint string) *Error {
	return &Error{Code: "usage", Message: message, Hint: hint, Usage: true}
}

func configError(err error) error {
	if err == nil {
		return nil
	}
	return &Error{Code: "config", Message: err.Error()}
}
