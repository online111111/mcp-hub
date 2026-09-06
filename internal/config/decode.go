package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var (
	ErrConfigFileTooLarge = fmt.Errorf("config file exceeds maximum size limit of %d bytes", MaxConfigFileSize)
	ErrTrailingJSON       = errors.New("trailing JSON data detected after valid configuration")
	ErrEmptyConfig        = errors.New("configuration data is empty")
)

// CheckDuplicateKeys scans JSON tokens to detect duplicate keys in objects at any nesting level.
func CheckDuplicateKeys(data []byte) error {
	if len(bytes.TrimSpace(data)) == 0 {
		return ErrEmptyConfig
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	type state struct {
		isObject     bool
		expectingKey bool
		keys         map[string]struct{}
	}
	var stack []state

	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return fmt.Errorf("json token error: %w", err)
		}

		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{':
				stack = append(stack, state{
					isObject:     true,
					expectingKey: true,
					keys:         make(map[string]struct{}),
				})
			case '}':
				if len(stack) == 0 || !stack[len(stack)-1].isObject {
					return errors.New("mismatched '}' in JSON")
				}
				stack = stack[:len(stack)-1]
				// If parent was an object waiting for value, value is now complete
				if len(stack) > 0 && stack[len(stack)-1].isObject {
					stack[len(stack)-1].expectingKey = true
				}
			case '[':
				stack = append(stack, state{
					isObject:     false,
					expectingKey: false,
				})
			case ']':
				if len(stack) == 0 || stack[len(stack)-1].isObject {
					return errors.New("mismatched ']' in JSON")
				}
				stack = stack[:len(stack)-1]
				// If parent was an object waiting for value, value is now complete
				if len(stack) > 0 && stack[len(stack)-1].isObject {
					stack[len(stack)-1].expectingKey = true
				}
			}
		case string:
			if len(stack) > 0 && stack[len(stack)-1].isObject {
				s := &stack[len(stack)-1]
				if s.expectingKey {
					if _, exists := s.keys[t]; exists {
						return fmt.Errorf("duplicate JSON key %q detected", t)
					}
					s.keys[t] = struct{}{}
					s.expectingKey = false
				} else {
					// String was a value
					s.expectingKey = true
				}
			}
		default:
			// Number, bool, nil
			if len(stack) > 0 && stack[len(stack)-1].isObject {
				stack[len(stack)-1].expectingKey = true
			}
		}
	}

	if len(stack) != 0 {
		return errors.New("unexpected end of JSON input: unclosed object or array")
	}

	return nil
}

// DecodeStrict decodes JSON bytes into v while enforcing:
// 1. File size limit (<= 1 MiB)
// 2. Duplicate key rejection at all object levels
// 3. Unknown field rejection
// 4. Trailing data rejection (must end at EOF)
func DecodeStrict(data []byte, v any) error {
	if len(data) > MaxConfigFileSize {
		return ErrConfigFileTooLarge
	}

	if err := CheckDuplicateKeys(data); err != nil {
		return fmt.Errorf("duplicate key check failed: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()

	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("JSON decode error: %w", err)
	}

	// Ensure there is no trailing data
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("%w: extra content found after root object", ErrTrailingJSON)
	}

	return nil
}
