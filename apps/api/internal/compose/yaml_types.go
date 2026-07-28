package compose

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// stringOrIntList accepts a YAML sequence of strings/ints, a single
// scalar, or nothing, normalizing all of them to a []string.
type stringOrIntList struct {
	values []string
}

func (l *stringOrIntList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		for _, item := range node.Content {
			l.values = append(l.values, item.Value)
		}
	case yaml.ScalarNode:
		if node.Value != "" {
			l.values = []string{node.Value}
		}
	}
	return nil
}

// stringOrList accepts either a single string (shell form) or a
// sequence of strings (exec form), joining the latter with spaces.
type stringOrList struct {
	value string
}

func (l *stringOrList) joined() string { return l.value }

func (l *stringOrList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		l.value = node.Value
	case yaml.SequenceNode:
		var parts []string
		for _, item := range node.Content {
			parts = append(parts, item.Value)
		}
		l.value = strings.Join(parts, " ")
	}
	return nil
}

// stringOrMapKeys accepts either a sequence of strings (depends_on's
// short form) or a mapping whose keys are the values wanted (long form
// with per-dependency conditions, which are discarded — only the
// dependency name is structural evidence, not the health condition).
type stringOrMapKeys struct {
	values []string
}

func (l *stringOrMapKeys) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		for _, item := range node.Content {
			l.values = append(l.values, item.Value)
		}
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			l.values = append(l.values, node.Content[i].Value)
		}
	}
	return nil
}

// envList accepts either a sequence of "KEY=VALUE"/"KEY" strings or a
// mapping of KEY: VALUE, extracting only the KEY names. Values are
// never retained — spec §7.4 forbids displaying secret values, and an
// env var's value in a compose file may be a literal secret.
type envList struct {
	names []string
}

func (l *envList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		for _, item := range node.Content {
			name, _, _ := strings.Cut(item.Value, "=")
			if name != "" {
				l.names = append(l.names, name)
			}
		}
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			if name := node.Content[i].Value; name != "" {
				l.names = append(l.names, name)
			}
		}
	}
	return nil
}
