package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

const MaxEnvFileBytes = 64 * 1024

// ReadEnvFile reads a regular, bounded env file without blocking on special files.
// Relative paths are based at project, matching job env_file semantics.
func ReadEnvFile(project, configuredPath string) (map[string]string, error) {
	path := configuredPath
	if !filepath.IsAbs(path) {
		path = filepath.Join(project, path)
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxEnvFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxEnvFileBytes {
		return nil, errors.New("exceeds 64 KiB")
	}
	return ParseEnvFile(data)
}

// ParseEnvFile parses Noon's non-interpolating, single-line Compose env subset.
// Diagnostics contain only line numbers and fixed descriptions, never secrets.
func ParseEnvFile(data []byte) (map[string]string, error) {
	values := make(map[string]string)
	for index, raw := range strings.Split(string(data), "\n") {
		fail := func(message string) (map[string]string, error) {
			return nil, fmt.Errorf("line %d: %s", index+1, message)
		}
		if !utf8.ValidString(raw) || strings.ContainsRune(raw, 0) {
			return fail("must be UTF-8 text without NUL")
		}
		line := strings.TrimLeftFunc(strings.TrimSuffix(raw, "\r"), unicode.IsSpace)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "export ") || strings.HasPrefix(line, "export\t") {
			line = strings.TrimLeft(line[len("export"):], " \t")
		}
		key, text, ok := strings.Cut(line, "=")
		key = strings.TrimRight(key, " \t")
		if !ok || !envName.MatchString(key) || strings.HasPrefix(key, "NOON_") {
			return fail("expected a valid, non-reserved KEY=value assignment")
		}
		value, err := envFileValue(strings.TrimLeft(text, " \t"))
		if err != nil {
			return fail(err.Error())
		}
		values[key] = value
	}
	return values, nil
}

func envFileValue(text string) (string, error) {
	invalid := func() (string, error) { return "", fmt.Errorf("invalid value syntax") }
	if text == "" {
		return "", nil
	}
	// Only spaces/tabs are allowed before a value. In particular, do not
	// accept other leading whitespace as literal text that Compose would trim.
	first, _ := utf8.DecodeRuneInString(text)
	if unicode.IsSpace(first) {
		return invalid()
	}
	quote := text[0]
	if quote != '\'' && quote != '"' {
		if index := strings.Index(text, " #"); index >= 0 {
			text = text[:index]
		}
		if strings.ContainsAny(text, "$\r") {
			return invalid()
		}
		return strings.TrimRightFunc(text, unicode.IsSpace), nil
	}
	var value strings.Builder
	for i := 1; i < len(text); i++ {
		ch := text[i]
		if ch == quote {
			rest := strings.TrimLeftFunc(text[i+1:], unicode.IsSpace)
			if rest != "" && !strings.HasPrefix(rest, "#") {
				return invalid()
			}
			return value.String(), nil
		}
		if ch == '\r' || quote == '"' && ch == '$' {
			return invalid()
		}
		if ch == '\\' && i+1 < len(text) {
			next := text[i+1]
			if quote == '\'' {
				if next != '\'' {
					value.WriteByte(ch)
				}
				value.WriteByte(next)
				i++
				continue
			} else {
				switch next {
				case 'n':
					ch = '\n'
				case 'r':
					ch = '\r'
				case 't':
					ch = '\t'
				case '"', '\\':
					ch = next
				default:
					return invalid()
				}
				i++
			}
		}
		value.WriteByte(ch)
	}
	return invalid()
}
