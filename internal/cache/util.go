package cache

import (
	"encoding/json"
	"errors"
)

// ErrNotFound is returned by store helpers when a key does not exist.
var ErrNotFound = errors.New("cache: not found")

// encodeJSON marshals a value for Redis storage.
func encodeJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// decodeJSON unmarshals a value previously stored via encodeJSON / Set.
func decodeJSON(b []byte, v any) error {
	return json.Unmarshal(b, v)
}
