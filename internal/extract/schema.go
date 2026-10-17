package extract

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/pinchtab/pinchtab/internal/selector"
)

type Type string

const (
	TypeString  Type = "string"
	TypeNumber  Type = "number"
	TypeInteger Type = "integer"
	TypeBoolean Type = "boolean"
	TypeArray   Type = "array"
)

type target struct {
	kind  targetKind
	value string
}

type targetKind int

const (
	targetNone targetKind = iota
	targetQuery
	targetRef
)

type Property struct {
	Name        string
	Type        Type
	Description string
	Required    bool
	Hint        string
	Items       *Schema
	Scope       string
	MinItems    int
	MaxItems    int

	hint  target
	scope target
}

type Schema struct {
	Properties []Property

	scope target
}

func (s Schema) WithScope(raw string) (Schema, error) {
	scope, err := parseTarget(strings.TrimSpace(raw), "scope")
	if err != nil {
		return Schema{}, err
	}
	s.scope = scope
	return s, nil
}

type UnsupportedError struct {
	Path   string
	Reason string
}

func (e *UnsupportedError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, e.Reason)
}

type rawSchema struct {
	Type       string                 `json:"type"`
	Required   []string               `json:"required"`
	Properties map[string]rawProperty `json:"properties"`
}

type rawProperty struct {
	Type        string          `json:"type"`
	Description string          `json:"description"`
	Hint        string          `json:"x-pinchtab-hint"`
	Scope       string          `json:"x-pinchtab-scope"`
	Properties  json.RawMessage `json:"properties"`
	Items       json.RawMessage `json:"items"`
	MinItems    int             `json:"minItems"`
	MaxItems    int             `json:"maxItems"`
}

func ParseSchema(data []byte) (Schema, error) {
	return parseObject(data, "", true)
}

func parseObject(data []byte, path string, allowArrays bool) (Schema, error) {
	var raw rawSchema
	if err := json.Unmarshal(data, &raw); err != nil {
		return Schema{}, fmt.Errorf("parse schema: %w", err)
	}
	if raw.Type != "" && raw.Type != "object" {
		return Schema{}, &UnsupportedError{Path: join(path, "type"), Reason: raw.Type + " is not supported"}
	}
	if len(raw.Properties) == 0 {
		return Schema{}, &UnsupportedError{Path: join(path, "properties"), Reason: "an object schema with properties is required"}
	}

	required := make(map[string]bool, len(raw.Required))
	for _, name := range raw.Required {
		required[name] = true
	}

	names := make([]string, 0, len(raw.Properties))
	for name := range raw.Properties {
		names = append(names, name)
	}
	sort.Strings(names)

	props := make([]Property, 0, len(names))
	for _, name := range names {
		prop, err := parseProperty(join(path, "properties."+name), name, raw.Properties[name], required[name], allowArrays)
		if err != nil {
			return Schema{}, err
		}
		props = append(props, prop)
	}
	return Schema{Properties: props}, nil
}

func join(path, leaf string) string {
	if path == "" {
		return leaf
	}
	return path + "." + leaf
}

func parseProperty(path, name string, rp rawProperty, required, allowArrays bool) (Property, error) {
	switch Type(rp.Type) {
	case TypeString, TypeNumber, TypeInteger, TypeBoolean:
	case TypeArray:
		if !allowArrays {
			return Property{}, &UnsupportedError{Path: path + ".type", Reason: "nested arrays are not supported"}
		}
	case "":
		return Property{}, &UnsupportedError{Path: path + ".type", Reason: "a property type is required"}
	default:
		return Property{}, &UnsupportedError{Path: path + ".type", Reason: rp.Type + " is not supported"}
	}
	if len(rp.Properties) > 0 {
		return Property{}, &UnsupportedError{Path: path + ".properties", Reason: "nested object properties are not supported"}
	}
	if rp.MinItems < 0 || rp.MaxItems < 0 {
		return Property{}, &UnsupportedError{Path: path + ".maxItems", Reason: "item bounds must not be negative"}
	}

	prop := Property{
		Name:        name,
		Type:        Type(rp.Type),
		Description: strings.TrimSpace(rp.Description),
		Required:    required,
		Hint:        strings.TrimSpace(rp.Hint),
		Scope:       strings.TrimSpace(rp.Scope),
		MinItems:    rp.MinItems,
		MaxItems:    rp.MaxItems,
	}
	var err error
	if prop.hint, err = parseTarget(prop.Hint, path+".x-pinchtab-hint"); err != nil {
		return Property{}, err
	}
	if prop.scope, err = parseTarget(prop.Scope, path+".x-pinchtab-scope"); err != nil {
		return Property{}, err
	}
	if prop.Type == TypeArray {
		items, err := parseItems(rp.Items, path+".items")
		if err != nil {
			return Property{}, err
		}
		prop.Items = &items
	}
	return prop, nil
}

func parseItems(raw json.RawMessage, path string) (Schema, error) {
	if len(raw) == 0 {
		return Schema{}, &UnsupportedError{Path: path, Reason: "array items must be an object schema"}
	}
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return Schema{}, fmt.Errorf("parse schema: %w", err)
	}
	if head.Type != "object" {
		return Schema{}, &UnsupportedError{Path: path + ".type", Reason: "array items must be an object schema"}
	}
	return parseObject(raw, path, false)
}

func parseTarget(raw, path string) (target, error) {
	if raw == "" {
		return target{}, nil
	}
	sel := selector.Parse(raw)
	if sel.Kind == selector.KindCSS && !selector.HasKnownPrefix(raw) {
		return target{kind: targetQuery, value: raw}, nil
	}

	switch sel.Kind {
	case selector.KindCSS, selector.KindXPath:
		return target{}, &UnsupportedError{Path: path, Reason: string(sel.Kind) + " selectors need a browser and are not supported"}
	case selector.KindText:
		return target{kind: targetQuery, value: sel.Value}, nil
	case selector.KindRef:
		return target{kind: targetRef, value: sel.Value}, nil
	}

	query, ok := sel.SemanticQuery()
	if !ok {
		return target{}, &UnsupportedError{Path: path, Reason: string(sel.Kind) + " selectors are not supported"}
	}
	return target{kind: targetQuery, value: query}, nil
}
