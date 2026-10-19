package config

import (
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// resolveYAML retains source locations while expanding file-local composition.
// Decoding into Go maps would lose locations and explicit duplicate keys before
// our schema validators could inspect them. Merge precedence follows YAML:
// explicit keys win, then the first mapping in a merge sequence wins.
func resolveYAML(root *yaml.Node) (*yaml.Node, error) {
	const maxNodes = 100000
	const maxBytes = 8 << 20
	active := make(map[*yaml.Node]bool)
	nodes, bytes := 0, 0
	var resolve func(*yaml.Node, string, int) (*yaml.Node, error)
	resolve = func(node *yaml.Node, path string, depth int) (*yaml.Node, error) {
		fail := func(message string) (*yaml.Node, error) {
			if path != "" {
				message = strings.Trim(strconv.Quote(path), "\"") + ": " + message
			}
			return nil, &yamlDiagnostic{line: node.Line, message: message}
		}
		nodes++
		bytes += len(node.Value)
		if nodes > maxNodes || bytes > maxBytes || depth > 100 {
			return fail("YAML expansion limit exceeded (100000 nodes, 8 MiB, depth 100)")
		}
		if active[node] {
			return fail("cyclic YAML alias")
		}
		active[node] = true
		defer delete(active, node)
		if node.Kind == yaml.AliasNode {
			return resolve(node.Alias, path, depth+1)
		}
		switch node.Tag {
		case "!!map", "!!seq", "!!str", "!!int", "!!float", "!!bool", "!!null", "!!timestamp", "!!merge":
		default:
			return fail("unsupported YAML tag")
		}
		out := *node
		out.Anchor, out.Alias, out.Content = "", nil, nil
		if node.Kind != yaml.MappingNode {
			for _, child := range node.Content {
				value, err := resolve(child, path, depth+1)
				if err != nil {
					return nil, err
				}
				out.Content = append(out.Content, value)
			}
			return &out, nil
		}
		explicit := make(map[string]bool)
		mergeSeen := false
		var merged []*yaml.Node
		for i := 0; i < len(node.Content); i += 2 {
			key, err := resolve(node.Content[i], path, depth+1)
			if err != nil {
				return nil, err
			}
			value := node.Content[i+1]
			if key.Kind != yaml.ScalarNode || (key.Tag != "!!str" && key.Tag != "!!merge") {
				return fail("configuration keys must be strings")
			}
			field := key.Value
			if path != "" {
				field = path + "." + field
			}
			isMerge := key.Tag == "!!merge"
			if (isMerge && mergeSeen) || (!isMerge && explicit[key.Value]) {
				return nil, &yamlDiagnostic{line: key.Line, message: strings.Trim(strconv.Quote(field), "\"") + ": duplicate field"}
			}
			if isMerge {
				mergeSeen = true
			} else {
				explicit[key.Value] = true
			}
			resolved, err := resolve(value, field, depth+1)
			if err != nil {
				return nil, err
			}
			if key.Tag == "!!merge" {
				if resolved.Kind == yaml.SequenceNode {
					merged = append(merged, resolved.Content...)
				} else {
					merged = append(merged, resolved)
				}
				continue
			}
			out.Content = append(out.Content, key, resolved)
		}
		for _, source := range merged {
			if source.Kind != yaml.MappingNode {
				return fail("YAML merge requires a mapping or a sequence of mappings")
			}
			for i := 0; i < len(source.Content); i += 2 {
				key := source.Content[i]
				if !explicit[key.Value] {
					out.Content = append(out.Content, key, source.Content[i+1])
					explicit[key.Value] = true
				}
			}
		}
		return &out, nil
	}
	return resolve(root, "", 0)
}

// Source-splicing editors cannot safely remove anchor definitions or edit jobs
// inherited through merges. Fail closed rather than rewrite unrelated content.
func hasYAMLComposition(node *yaml.Node) bool {
	if node.Anchor != "" || node.Kind == yaml.AliasNode || node.Tag == "!!merge" {
		return true
	}
	for _, child := range node.Content {
		if hasYAMLComposition(child) {
			return true
		}
	}
	return false
}
