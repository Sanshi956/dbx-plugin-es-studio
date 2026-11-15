package mcp

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

// Segments are the only user-controlled path pieces in the whole tool layer.
// Everything is validated against a whitelist and then escaped, so no tool can
// escape its path, inject a query string, or point at another host
// (requirement 10.2).
const (
	nameChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.*-+,"
	taskChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.:-"
)

func segment(value, what string, allowed string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", invalid("%s is required", what)
	}
	if len(value) > 255 {
		return "", invalid("%s is too long", what)
	}
	for _, r := range value {
		if !strings.ContainsRune(allowed, r) {
			return "", invalid("%s contains an unsupported character %q", what, r)
		}
	}
	if strings.Contains(value, "..") {
		return "", invalid("%s must not contain ..", what)
	}
	return value, nil
}

// name validates an index / alias / data stream / template identifier or
// pattern (`logs-*`, `a,b`, `_all` all stay usable).
func name(arg, what string) (string, error) { return segment(arg, what, nameChars) }

// docID keeps the full byte range but escapes it: document ids are arbitrary
// strings, and only the path separator must be neutralised.
func docID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", invalid("id is required")
	}
	if len(value) > 512 {
		return "", invalid("id is too long")
	}
	return url.PathEscape(value), nil
}

// keyword validates sort/query-string style tokens.
func keyword(value, what string) (string, error) { return segment(value, what, nameChars+" :") }

func body(a Args, name string) ([]byte, error) {
	object := a.Object(name)
	if len(object) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, invalid("%s is not valid JSON", name)
	}
	return encoded, nil
}

// withLimit appends a size/limit parameter only when the caller asked for one.
func withLimit(query map[string]string, size int) map[string]string {
	if size > 0 {
		query["size"] = fmt.Sprint(size)
	}
	return query
}

// catHeader is the column list used for every _cat call. `format=json` plus an
// explicit `h=` keeps the response stable across ES versions instead of relying
// on the version-specific default column set.
func catPath(api string, columns string, extra map[string]string) Request {
	query := map[string]string{"format": "json"}
	if columns != "" {
		query["h"] = columns
	}
	for key, value := range extra {
		query[key] = value
	}
	return Request{Method: "GET", Path: "/_cat/" + api, Query: query}
}
