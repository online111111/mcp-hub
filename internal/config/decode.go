package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
)

var (
	ErrConfigFileTooLarge = fmt.Errorf("config file exceeds maximum size limit of %d bytes", MaxConfigFileSize)
	ErrTrailingJSON       = errors.New("trailing JSON data detected after valid configuration")
	ErrEmptyConfig        = errors.New("configuration data is empty")
)

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

func stripUTF8BOM(data []byte) []byte {
	return bytes.TrimPrefix(data, utf8BOM)
}

// CheckDuplicateKeys scans JSON tokens to detect duplicate keys in objects at any nesting level.
func CheckDuplicateKeys(data []byte) error {
	data = stripUTF8BOM(data)
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
				stack = append(stack, state{isObject: true, expectingKey: true, keys: make(map[string]struct{})})
			case '}':
				if len(stack) == 0 || !stack[len(stack)-1].isObject {
					return errors.New("mismatched '}' in JSON")
				}
				stack = stack[:len(stack)-1]
				if len(stack) > 0 && stack[len(stack)-1].isObject {
					stack[len(stack)-1].expectingKey = true
				}
			case '[':
				stack = append(stack, state{isObject: false})
			case ']':
				if len(stack) == 0 || stack[len(stack)-1].isObject {
					return errors.New("mismatched ']' in JSON")
				}
				stack = stack[:len(stack)-1]
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
					s.expectingKey = true
				}
			}
		default:
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

// validateExactJSONFieldNames rejects the case-insensitive struct-field matching
// performed by encoding/json. Configuration field names are a wire contract and
// must exactly match their json tags (for example publicMode, not PublicMode).
func validateExactJSONFieldNames(data []byte, target any) error {
	var value any
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&value); err != nil {
		return err
	}
	t := reflect.TypeOf(target)
	if t == nil {
		return errors.New("decode target type is nil")
	}
	return validateJSONShape(value, t, "$")
}

func validateJSONShape(value any, t reflect.Type, path string) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	switch t.Kind() {
	case reflect.Struct:
		obj, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		fields := make(map[string]reflect.Type)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			tag := f.Tag.Get("json")
			name := strings.Split(tag, ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			fields[name] = f.Type
		}
		for key, child := range obj {
			fieldType, ok := fields[key]
			if !ok {
				return fmt.Errorf("JSON field %s.%s does not exactly match an allowed field name", path, key)
			}
			if err := validateJSONShape(child, fieldType, path+"."+key); err != nil {
				return err
			}
		}

	case reflect.Map:
		obj, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		for key, child := range obj {
			if err := validateJSONShape(child, t.Elem(), path+"."+key); err != nil {
				return err
			}
		}

	case reflect.Slice, reflect.Array:
		arr, ok := value.([]any)
		if !ok {
			return nil
		}
		for i, child := range arr {
			if err := validateJSONShape(child, t.Elem(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

// DecodeStrict decodes JSON bytes into v while enforcing:
// 1. File size limit (<= 1 MiB)
// 2. Duplicate key rejection at all object levels
// 3. Exact, case-sensitive field-name matching
// 4. Unknown field rejection
// 5. Trailing data rejection (must end at EOF)
// A single leading UTF-8 BOM is accepted for Windows/editor compatibility.
func DecodeStrict(data []byte, v any) error {
	if len(data) > MaxConfigFileSize {
		return ErrConfigFileTooLarge
	}
	data = stripUTF8BOM(data)
	if err := CheckDuplicateKeys(data); err != nil {
		return fmt.Errorf("duplicate key check failed: %w", err)
	}
	if err := validateExactJSONFieldNames(data, v); err != nil {
		return fmt.Errorf("exact field-name check failed: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("JSON decode error: %w", err)
	}
	var trailing any
	if err := dec.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("%w: extra content found after root object", ErrTrailingJSON)
	}
	return nil
}
