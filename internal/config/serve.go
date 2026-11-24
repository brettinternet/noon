package config

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Serve holds listener settings. Only user config may set them, so cloned or
// pulled repository content can never open a port.
type Serve struct {
	Listen       string `json:"listen,omitempty"`
	APITokenFile string `json:"api_token_file,omitempty"`
	WebTokenFile string `json:"web_token_file,omitempty"`
	// PublicURL is the web viewer's external base URL, without a trailing slash.
	PublicURL string `json:"public_url,omitempty"`
}

// MinTokenBytes is the shortest accepted bearer token.
const MinTokenBytes = 32

// ServeSettings resolves the state directory and listener settings from user
// config without discovering or loading a project. Pairing is left to the
// caller, which may combine settings with flags.
func ServeSettings(input Input) (stateDir string, serve Serve, err error) {
	cfg, err := userSettings(input)
	return cfg.StateDir, cfg.Serve, err
}

func (cfg *Config) applyServe(path string, node *yaml.Node, requirePair bool) error {
	if node.Kind != yaml.MappingNode {
		return jobError(path, node, "serve", "must be a mapping")
	}
	var serve Serve
	for i := 0; i+1 < len(node.Content); i += 2 {
		key, value := node.Content[i], node.Content[i+1]
		field := "serve." + key.Value
		if value.Kind != yaml.ScalarNode || value.Tag != "!!str" || strings.TrimSpace(value.Value) == "" {
			return jobError(path, value, field, "must be a non-empty string")
		}
		switch key.Value {
		case "listen":
			serve.Listen = value.Value
		case "public_url":
			public, err := parsePublicURL(value.Value)
			if err != nil {
				return jobError(path, value, field, err.Error())
			}
			serve.PublicURL = public
		case "api_token_file", "web_token_file":
			file, err := expandHome(value.Value)
			if err != nil {
				return jobError(path, value, field, err.Error())
			}
			if key.Value == "api_token_file" {
				serve.APITokenFile = resolvePath(filepath.Dir(path), file)
			} else {
				serve.WebTokenFile = resolvePath(filepath.Dir(path), file)
			}
		default:
			return jobError(path, key, field, "unknown field")
		}
	}
	if serve.PublicURL != "" && serve.WebTokenFile == "" {
		return jobError(path, node, "serve.public_url", "requires serve.web_token_file")
	}
	if requirePair && serve.Listen == "" {
		if serve.APITokenFile != "" {
			return jobError(path, node, "serve.api_token_file", "requires serve.listen")
		}
		if serve.WebTokenFile != "" {
			return jobError(path, node, "serve.web_token_file", "requires serve.listen")
		}
	}
	cfg.Serve = serve
	return nil
}

// parsePublicURL accepts an absolute http(s) URL without a query or fragment
// and drops one trailing slash so run paths join cleanly.
func parsePublicURL(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return "", errors.New("must be an absolute http or https URL")
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || strings.Contains(raw, "#") {
		return "", errors.New("must not include a query or fragment")
	}
	return strings.TrimSuffix(raw, "/"), nil
}

// ErrTokenReuse reports a web token equal to the API token. The two grant
// different authority, so they must differ.
var ErrTokenReuse = errors.New("the web token must differ from the API token")

// ReadServeTokens reads each configured token file once and refuses a web
// token equal to the API token. Errors name files, never contents.
func ReadServeTokens(serve Serve) (api, web []byte, err error) {
	if serve.APITokenFile != "" {
		if api, err = ReadTokenFile(serve.APITokenFile); err != nil {
			return nil, nil, err
		}
	}
	if serve.WebTokenFile != "" {
		if web, err = ReadTokenFile(serve.WebTokenFile); err != nil {
			return nil, nil, err
		}
	}
	if api != nil && web != nil && subtle.ConstantTimeCompare(api, web) == 1 {
		return nil, nil, ErrTokenReuse
	}
	return api, web, nil
}

// ReadTokenFile reads a bearer token once. One trailing newline is dropped.
// Errors name the file, never its contents.
func ReadTokenFile(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read token file %q: %w", path, err)
	}
	value := string(data)
	if strings.HasSuffix(value, "\r\n") {
		value = strings.TrimSuffix(value, "\r\n")
	} else {
		value = strings.TrimSuffix(value, "\n")
	}
	if len(value) < MinTokenBytes {
		return nil, fmt.Errorf("token file %q must hold at least %d bytes", path, MinTokenBytes)
	}
	return []byte(value), nil
}
