package main

// Caller Agent settings files are JSON that people edit by hand. Adding a hook
// entry must leave every existing byte where it was, so the appender reads the
// objects along its path as ordered keys with raw value spans and splices the
// new value in after the last existing child, in the file's own indentation.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// jsonText is serialized JSON: a whole settings document or one encoded
// value.
type jsonText []byte

// jsonPath is the object keys leading from a document's root to a value.
type jsonPath []string

// jsonKind is the opening bracket of a JSON container.
type jsonKind byte

const (
	jsonObject jsonKind = '{'
	jsonArray  jsonKind = '['
)

func (kind jsonKind) String() string {
	if kind == jsonObject {
		return "object"
	}
	return "array"
}

// jsonContainer is one JSON object or array in a document: the offsets of its
// brackets and of each child value, with the key of each object member.
type jsonContainer struct {
	open, close int
	children    []jsonChild
}

type jsonChild struct {
	key        string
	start, end int
}

// appendJSONArrayElement appends element to the array found by following
// path's object keys from the document root, creating missing keys and an
// empty document. Every existing byte is kept.
func appendJSONArrayElement(document jsonText, path jsonPath, element any) ([]byte, error) {
	if len(bytes.TrimSpace(document)) == 0 {
		value, err := jsonLayout{multiline: true, unit: "  "}.encode(path.value(element))
		return append(value, '\n'), err
	}
	container, err := document.container(jsonChild{}, jsonObject)
	if err != nil {
		return nil, fmt.Errorf("the document is not a JSON object: %w", err)
	}
	for depth, key := range path {
		child, found := container.member(key)
		if !found {
			return container.insert(document, path[depth:], element)
		}
		kind := jsonObject
		if depth == len(path)-1 {
			kind = jsonArray
		}
		if container, err = document.container(child, kind); err != nil {
			return nil, fmt.Errorf("%s: %w", strings.Join(path[:depth+1], "."), err)
		}
	}
	return container.insert(document, nil, element)
}

// value is what the missing keys of path hold: objects down to an array
// holding element.
func (path jsonPath) value(element any) any {
	if len(path) == 0 {
		return []any{element}
	}
	return map[string]any{path[0]: path[1:].value(element)}
}

// container reads the container that child's value holds; the zero child
// stands for the document root.
func (document jsonText) container(child jsonChild, kind jsonKind) (jsonContainer, error) {
	scanner := jsonScanner{decoder: json.NewDecoder(bytes.NewReader(document[child.start:])), start: child.start}
	token, err := scanner.decoder.Token()
	if err != nil {
		return jsonContainer{}, err
	}
	if delimiter, ok := token.(json.Delim); !ok || jsonKind(delimiter) != kind {
		return jsonContainer{}, fmt.Errorf("want a JSON %s", kind)
	}
	container := jsonContainer{open: scanner.offset() - 1}
	for scanner.decoder.More() {
		child, err := scanner.child(kind)
		if err != nil {
			return jsonContainer{}, err
		}
		container.children = append(container.children, child)
	}
	if _, err := scanner.decoder.Token(); err != nil {
		return jsonContainer{}, err
	}
	container.close = scanner.offset() - 1
	return container, nil
}

// jsonScanner decodes a document from offset start.
type jsonScanner struct {
	decoder *json.Decoder
	start   int
}

func (scanner jsonScanner) offset() int { return scanner.start + int(scanner.decoder.InputOffset()) }

// child reads the next member of an object or element of an array.
func (scanner jsonScanner) child(kind jsonKind) (jsonChild, error) {
	var child jsonChild
	if kind == jsonObject {
		key, err := scanner.decoder.Token()
		if err != nil {
			return child, err
		}
		child.key = fmt.Sprint(key)
	}
	var raw json.RawMessage
	if err := scanner.decoder.Decode(&raw); err != nil {
		return child, err
	}
	child.end = scanner.offset()
	child.start = child.end - len(raw)
	return child, nil
}

func (container jsonContainer) member(key string) (jsonChild, bool) {
	for _, child := range container.children {
		if child.key == key {
			return child, true
		}
	}
	return jsonChild{}, false
}

// jsonLayout is how a new child is written into a container: on its own line
// at childIndent, nesting by unit, or compact when the container is on one
// line, which leaves both indents empty.
type jsonLayout struct {
	multiline          bool
	outer, childIndent string
	unit               string
}

// layout follows the container's existing children, or the document's own
// indentation when it has none.
func (container jsonContainer) layout(document jsonText) jsonLayout {
	layout := jsonLayout{outer: document.lineIndent(container.open), multiline: bytes.Contains(document, []byte("\n"))}
	layout.childIndent = layout.outer + document.indentUnit()
	if len(container.children) > 0 {
		first := container.children[0].start
		layout.multiline = bytes.Contains(document[container.open:first], []byte("\n"))
		layout.childIndent = document.lineIndent(first)
	}
	unit, nested := strings.CutPrefix(layout.childIndent, layout.outer)
	if !nested || unit == "" {
		unit = document.indentUnit()
	}
	layout.unit = unit
	if !layout.multiline {
		return jsonLayout{}
	}
	return layout
}

// insert adds element after the container's last child, in its layout.
func (container jsonContainer) insert(document jsonText, missing jsonPath, element any) ([]byte, error) {
	layout := container.layout(document)
	text, err := layout.addition(missing, element)
	if err != nil {
		return nil, err
	}
	return container.splice(document, text, layout), nil
}

// addition renders element, or when keys are missing, a member named by the
// first one holding the rest of the path down to element.
func (layout jsonLayout) addition(missing jsonPath, element any) (jsonText, error) {
	if len(missing) == 0 {
		return layout.encode(element)
	}
	name, err := jsonLayout{}.encode(missing[0])
	if err != nil {
		return nil, err
	}
	value, err := layout.encode(missing[1:].value(element))
	if err != nil {
		return nil, err
	}
	separator := ":"
	if layout.multiline {
		separator = ": "
	}
	return append(append(name, separator...), value...), nil
}

// encode renders value without HTML escaping, so shell commands keep their >
// and & as written.
func (layout jsonLayout) encode(value any) (jsonText, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if layout.multiline {
		encoder.SetIndent(layout.childIndent, layout.unit)
	}
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buffer.Bytes(), []byte("\n")), nil
}

// splice writes text after the last child, or into an empty container in
// place of whatever whitespace it held.
func (container jsonContainer) splice(document, text jsonText, layout jsonLayout) []byte {
	start, end, before, after := container.open+1, container.close, "", ""
	if count := len(container.children); count > 0 {
		start = container.children[count-1].end
		end, before = start, ","
	}
	if layout.multiline {
		before += "\n" + layout.childIndent
	}
	if layout.multiline && len(container.children) == 0 {
		after = "\n" + layout.outer
	}
	var spliced bytes.Buffer
	spliced.Write(document[:start])
	spliced.WriteString(before)
	spliced.Write(text)
	spliced.WriteString(after)
	spliced.Write(document[end:])
	return spliced.Bytes()
}

// lineIndent is the leading whitespace of the line holding offset.
func (document jsonText) lineIndent(offset int) string {
	line := document[bytes.LastIndexByte(document[:offset], '\n')+1:]
	return string(line[:len(line)-len(bytes.TrimLeft(line, " \t"))])
}

// indentUnit is the indentation of the first indented line, two spaces when
// no line is indented.
func (document jsonText) indentUnit() string {
	for _, line := range bytes.Split(document, []byte("\n")) {
		if trimmed := bytes.TrimLeft(line, " \t"); len(trimmed) > 0 && len(trimmed) < len(line) {
			return string(line[:len(line)-len(trimmed)])
		}
	}
	return "  "
}
