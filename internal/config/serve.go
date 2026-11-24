package config

import (
	"fmt"
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
		case "api_token_file":
			file, err := expandHome(value.Value)
			if err != nil {
				return jobError(path, value, field, err.Error())
			}
			serve.APITokenFile = resolvePath(filepath.Dir(path), file)
		default:
			return jobError(path, key, field, "unknown field")
		}
	}
	if requirePair && serve.APITokenFile != "" && serve.Listen == "" {
		return jobError(path, node, "serve.api_token_file", "requires serve.listen")
	}
	cfg.Serve = serve
	return nil
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
