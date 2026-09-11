// Package nodepacket decodes untrusted packets at the node command boundary.
// It enforces the JSON wire shape separately from contract validation.
package nodepacket

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"

	"ownscout/internal/contract"
)

const (
	// MaxInputBytes is the inclusive packet size limit, including whitespace.
	MaxInputBytes = 1 << 20

	// RuleDecode identifies a wire-format failure returned by DecodeValid.
	// Contract validation uses the rule identifiers defined by contract.
	RuleDecode = "packet_decode"
)

// Decode reads exactly one JSON object of at most MaxInputBytes bytes.
// Field names must match contract's JSON tags exactly, including explicitly
// modeled compatibility spellings. Duplicate keys, unknown keys, invalid
// Unicode, trailing data, and values of the wrong JSON type are rejected.
// Explicit null is not accepted; omitted fields are left at their zero values.
//
// Decode does not validate the packet contract; use DecodeValid for that.
// It never modifies data and returns a zero Packet on error. Errors retain
// only fixed messages and canonical field paths, never input or parser errors.
func Decode(data []byte) (contract.Packet, error) {
	if len(data) > MaxInputBytes {
		return contract.Packet{}, reject("", "input exceeds 1 MiB")
	}
	if !utf8.Valid(data) {
		return contract.Packet{}, reject("", "input is not valid UTF-8")
	}
	if !validUnicodeEscapes(data) {
		return contract.Packet{}, reject("", "invalid Unicode escape")
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := checkValue(decoder, reflect.TypeOf(contract.Packet{}), ""); err != nil {
		return contract.Packet{}, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return contract.Packet{}, reject("", "unexpected data after packet")
	}

	var packet contract.Packet
	if err := json.Unmarshal(data, &packet); err != nil {
		// Never wrap encoding/json errors: they can quote packet values.
		return contract.Packet{}, reject("", "invalid field value")
	}
	return packet, nil
}

// DecodeValid decodes data and runs contract.ValidatePacket on a successful
// decode. It returns the decoded packet even when contract violations exist;
// callers must check len(violations) == 0 before consuming it.
//
// A decoding failure returns a zero Packet and one violation with RuleDecode,
// a canonical field path (or "packet" for the document), and a safe message.
// Contract validation is not run on a failed or partially decoded packet.
// Neither malformed input nor an invalid contract causes a panic.
func DecodeValid(data []byte) (contract.Packet, []contract.Violation) {
	packet, err := Decode(data)
	if err != nil {
		field := "packet"
		if boundary, ok := err.(*decodeError); ok {
			field = boundary.field
		}
		return contract.Packet{}, []contract.Violation{{
			Rule:    RuleDecode,
			Field:   field,
			Message: err.Error(),
		}}
	}
	return packet, contract.ValidatePacket(packet)
}

// checkValue follows only the fixed contract type graph, not an arbitrary
// input tree. Unexpected containers fail before recursion. UseNumber avoids
// float conversion, and the typed unmarshal happens only after this preflight.
func checkValue(decoder *json.Decoder, typ reflect.Type, path string) *decodeError {
	token, err := decoder.Token()
	if err != nil {
		return reject(path, "invalid JSON")
	}

	switch typ.Kind() {
	case reflect.Struct:
		if token != json.Delim('{') {
			return reject(path, "expected object")
		}
		seen := make(map[string]bool, typ.NumField())
		for decoder.More() {
			token, err := decoder.Token()
			if err != nil {
				return reject(path, "invalid JSON")
			}
			key, ok := token.(string)
			if !ok {
				return reject(path, "invalid JSON")
			}
			field, name, ok := exactField(typ, key)
			if !ok {
				// The unknown key itself is untrusted and must not be retained.
				return reject(path, "unknown field")
			}
			fieldPath := name
			if path != "" {
				fieldPath = path + "." + name
			}
			if seen[name] {
				return reject(fieldPath, "duplicate key")
			}
			seen[name] = true
			if err := checkValue(decoder, field.Type, fieldPath); err != nil {
				return err
			}
		}
		return checkClose(decoder, '}', path)

	case reflect.Slice:
		if token != json.Delim('[') {
			return reject(path, "expected array")
		}
		for index := 0; decoder.More(); index++ {
			if err := checkValue(decoder, typ.Elem(), path+"["+strconv.Itoa(index)+"]"); err != nil {
				return err
			}
		}
		return checkClose(decoder, ']', path)

	case reflect.String:
		if _, ok := token.(string); !ok {
			return reject(path, "expected string")
		}
	case reflect.Bool:
		if _, ok := token.(bool); !ok {
			return reject(path, "expected boolean")
		}
	case reflect.Int, reflect.Int64:
		number, ok := token.(json.Number)
		if !ok {
			return reject(path, "expected integer")
		}
		if _, err := strconv.ParseInt(string(number), 10, typ.Bits()); err != nil {
			// strconv errors also retain the input number; discard them.
			return reject(path, "integer is out of range or not integral")
		}
	default:
		// New contract types need an explicit wire-shape decision here.
		return reject(path, "unsupported contract field type")
	}
	return nil
}

func checkClose(decoder *json.Decoder, want json.Delim, path string) *decodeError {
	if token, err := decoder.Token(); err != nil || token != want {
		return reject(path, "invalid JSON")
	}
	return nil
}

// exactField deliberately does not reproduce encoding/json's case folding or
// implicit Go field names. The returned name comes from trusted type metadata.
func exactField(typ reflect.Type, key string) (reflect.StructField, string, bool) {
	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if field.IsExported() && name != "" && name != "-" && name == key {
			return field, name, true
		}
	}
	return reflect.StructField{}, "", false
}

// encoding/json replaces unpaired UTF-16 surrogate escapes with U+FFFD.
// Reject them before tokenization, while allowing literal escaped backslashes
// and actual U+FFFD characters. Other JSON syntax is checked by the decoder.
func validUnicodeEscapes(data []byte) bool {
	for index := 0; index < len(data); index++ {
		if data[index] != '\\' {
			continue
		}
		index++
		if index >= len(data) || data[index] != 'u' {
			continue
		}
		value, ok := hexEscape(data[index+1:])
		if !ok {
			return false
		}
		index += 4
		switch {
		case value >= 0xdc00 && value <= 0xdfff:
			return false
		case value >= 0xd800 && value <= 0xdbff:
			if index+6 >= len(data) || data[index+1] != '\\' || data[index+2] != 'u' {
				return false
			}
			low, ok := hexEscape(data[index+3:])
			if !ok || low < 0xdc00 || low > 0xdfff {
				return false
			}
			index += 6
		}
	}
	return true
}

func hexEscape(data []byte) (uint16, bool) {
	if len(data) < 4 {
		return 0, false
	}
	var value uint16
	for _, digit := range data[:4] {
		value <<= 4
		switch {
		case digit >= '0' && digit <= '9':
			value |= uint16(digit - '0')
		case digit >= 'a' && digit <= 'f':
			value |= uint16(digit-'a') + 10
		case digit >= 'A' && digit <= 'F':
			value |= uint16(digit-'A') + 10
		default:
			return 0, false
		}
	}
	return value, true
}

type decodeError struct {
	field  string
	reason string
}

func (err *decodeError) Error() string {
	return "nodepacket: " + err.field + ": " + err.reason
}

func reject(path, reason string) *decodeError {
	if path == "" {
		path = "packet"
	}
	return &decodeError{field: path, reason: reason}
}
