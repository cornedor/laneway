package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// SetUI writes one ui: option into the config file at path, through the
// YAML tree so comments and the other keys stay. A nil value removes the
// option (back to its default). A list is written in flow style.
func SetUI(path, name string, value any) error {
	// A dotfiles symlink stays one: write where it points.
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return fmt.Errorf("%s: not a mapping", path)
	}
	ui := mappingValue(root, "ui")
	switch {
	case ui == nil && value == nil:
		return nil
	case ui == nil:
		ui = &yaml.Node{Kind: yaml.MappingNode}
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: "ui"}, ui)
	case ui.Kind == yaml.ScalarNode && ui.Tag == "!!null": // a bare "ui:"
		ui.Kind, ui.Tag, ui.Value = yaml.MappingNode, "", ""
	case ui.Kind != yaml.MappingNode:
		return fmt.Errorf("%s: ui: is not a mapping", path)
	}
	if err := setMapping(ui, name, value); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return err
	}
	return writeFile(path, buf.Bytes())
}

// mappingValue is key's value node in m, nil when m lacks it.
func mappingValue(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// setMapping sets or (nil) removes key in m, keeping a replaced value's
// line comment.
func setMapping(m *yaml.Node, key string, value any) error {
	at := -1
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			at = i
		}
	}
	if value == nil {
		if at >= 0 {
			m.Content = append(m.Content[:at], m.Content[at+2:]...)
		}
		return nil
	}
	var v yaml.Node
	if err := v.Encode(value); err != nil {
		return err
	}
	if v.Kind == yaml.SequenceNode {
		v.Style = yaml.FlowStyle
	}
	if at < 0 {
		m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, &v)
		return nil
	}
	v.LineComment = m.Content[at+1].LineComment
	m.Content[at+1] = &v
	return nil
}

// writeFile replaces path through a temporary file beside it, keeping its
// mode, so a crash never leaves half a config.
func writeFile(path string, data []byte) error {
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".config-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
