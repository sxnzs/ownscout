package node

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"unicode/utf8"
)

// ParseEnvelope decodes exactly one object using exact, case-sensitive field
// names. All fields are required. Semantic checks requiring a packet belong to
// ValidateEnvelope; wire types, duplicate keys, arrays, and limits are checked
// here as well.
func ParseEnvelope(data []byte) (Envelope, error) {
	if len(data) > maxInputBytes {
		return Envelope{}, fmt.Errorf("envelope exceeds %d byte input limit", maxInputBytes)
	}
	if !utf8.Valid(data) {
		return Envelope{}, fmt.Errorf("envelope is not valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	var env Envelope
	err := readObject(decoder, map[string]func() error{
		"schema_version":        func() error { return readString(decoder, &env.SchemaVersion) },
		"envelope_id":           func() error { return readString(decoder, &env.EnvelopeID) },
		"packet_id":             func() error { return readString(decoder, &env.PacketID) },
		"packet_binding_sha256": func() error { return readString(decoder, &env.PacketBindingSHA256) },
		"nodes": func() error {
			env.Nodes = []Node{}
			return readArray(decoder, maxNodes, func() error {
				var n Node
				if err := readObject(decoder, map[string]func() error{
					"node_id":      func() error { return readString(decoder, &n.NodeID) },
					"depends_on":   func() error { return readStrings(decoder, &n.DependsOn) },
					"verifier":     func() error { return readString(decoder, &n.Verifier) },
					"evidence_ids": func() error { return readStrings(decoder, &n.EvidenceIDs) },
				}); err != nil {
					return err
				}
				env.Nodes = append(env.Nodes, n)
				return nil
			})
		},
	})
	if err != nil {
		return Envelope{}, fmt.Errorf("parse envelope: %w", err)
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return Envelope{}, fmt.Errorf("trailing JSON after envelope")
		}
		return Envelope{}, fmt.Errorf("trailing data after envelope: %w", err)
	}
	if err := validateLimits(env); err != nil {
		return Envelope{}, err
	}
	if len(env.Nodes) == 0 {
		return Envelope{}, fmt.Errorf("nodes must contain at least one node")
	}
	return env, nil
}

// Token-based decoding avoids encoding/json's last-key-wins and
// case-insensitive struct-field matching behavior, including escaped keys.
func readObject(decoder *json.Decoder, fields map[string]func() error) error {
	if err := readDelimiter(decoder, '{'); err != nil {
		return err
	}
	seen := make(map[string]bool, len(fields))
	for decoder.More() {
		var key string
		if err := readString(decoder, &key); err != nil {
			return err
		}
		if seen[key] {
			return fmt.Errorf("duplicate JSON object key %q", key)
		}
		read, exists := fields[key]
		if !exists {
			return fmt.Errorf("unknown field %q", key)
		}
		seen[key] = true
		if err := read(); err != nil {
			return fmt.Errorf("%s: %w", key, err)
		}
	}
	if err := readDelimiter(decoder, '}'); err != nil {
		return err
	}
	if len(seen) != len(fields) {
		var missing []string
		for key := range fields {
			if !seen[key] {
				missing = append(missing, key)
			}
		}
		sort.Strings(missing)
		return fmt.Errorf("missing required fields: %v", missing)
	}
	return nil
}

func readArray(decoder *json.Decoder, limit int, readElement func() error) error {
	if err := readDelimiter(decoder, '['); err != nil {
		return err
	}
	for count := 0; decoder.More(); count++ {
		if count >= limit {
			return fmt.Errorf("array exceeds %d element limit", limit)
		}
		if err := readElement(); err != nil {
			return fmt.Errorf("element %d: %w", count, err)
		}
	}
	return readDelimiter(decoder, ']')
}

func readStrings(decoder *json.Decoder, values *[]string) error {
	*values = []string{}
	return readArray(decoder, maxReferencesPerNode, func() error {
		var value string
		if err := readString(decoder, &value); err != nil {
			return err
		}
		*values = append(*values, value)
		return nil
	})
}

func readString(decoder *json.Decoder, value *string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	text, ok := token.(string)
	if !ok {
		return fmt.Errorf("expected a JSON string")
	}
	*value = text
	return nil
}

func readDelimiter(decoder *json.Decoder, want json.Delim) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token != want {
		return fmt.Errorf("expected JSON delimiter %q", want)
	}
	return nil
}
