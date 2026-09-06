package config

import (
	"fmt"
	"os"
	"strings"
)

// ErrMissingEnvVar is returned when an expanded variable is not set.
type ErrMissingEnvVar struct {
	Name string
}

func (e ErrMissingEnvVar) Error() string {
	return fmt.Sprintf("required environment variable %q is not set", e.Name)
}

// ExpandEnv expands ${VAR} references in s using the provided lookup function.
// It performs a strict single-pass expansion with the following rules:
// - ${NAME} expands to the value of NAME.
// - $${NAME} expands to the literal string ${NAME}.
// - $$ expands to a literal $.
// - Missing variables return ErrMissingEnvVar.
// - Recursive expansion is not performed.
func ExpandEnv(s string, lookup func(string) (string, bool)) (string, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}

	var buf strings.Builder
	buf.Grow(len(s))

	for i := 0; i < len(s); {
		if s[i] == '$' {
			// Check for $$
			if i+1 < len(s) && s[i+1] == '$' {
				// Check if it is $${...}
				if i+2 < len(s) && s[i+2] == '{' {
					end := strings.IndexByte(s[i+2:], '}')
					if end != -1 {
						// Escape: $${VAR} -> literal ${VAR}
						buf.WriteString(s[i+1 : i+2+end+1])
						i = i + 2 + end + 1
						continue
					}
				}
				// Literal $$ -> $
				buf.WriteByte('$')
				i += 2
				continue
			}

			// Check for ${VAR}
			if i+1 < len(s) && s[i+1] == '{' {
				end := strings.IndexByte(s[i+2:], '}')
				if end == -1 {
					return "", fmt.Errorf("unclosed environment variable reference at index %d", i)
				}
				varName := s[i+2 : i+2+end]
				if strings.TrimSpace(varName) == "" {
					return "", fmt.Errorf("empty environment variable name at index %d", i)
				}
				val, ok := lookup(varName)
				if !ok {
					return "", ErrMissingEnvVar{Name: varName}
				}
				buf.WriteString(val)
				i = i + 2 + end + 1
				continue
			}
		}

		buf.WriteByte(s[i])
		i++
	}

	return buf.String(), nil
}

// MergeEnv merges explicit server environment variables into the base environment.
// On Windows (or when isWindows=true), keys are matched case-insensitively so that,
// for example, an explicit PATH will overwrite an inherited Path instead of duplicating it.
func MergeEnv(base []string, explicit map[string]string, isWindows bool) map[string]string {
	result := make(map[string]string, len(base)+len(explicit))

	if isWindows {
		// Map lowercase key to canonical key currently in result
		canonicalKeys := make(map[string]string, len(base)+len(explicit))

		// First populate base environment
		for _, envEntry := range base {
			eq := strings.IndexByte(envEntry, '=')
			if eq <= 0 {
				continue
			}
			k := envEntry[:eq]
			v := envEntry[eq+1:]
			lowerK := strings.ToLower(k)

			result[k] = v
			canonicalKeys[lowerK] = k
		}

		// Overlay explicit environment
		for k, v := range explicit {
			lowerK := strings.ToLower(k)
			if oldKey, exists := canonicalKeys[lowerK]; exists && oldKey != k {
				// Remove older case variant
				delete(result, oldKey)
			}
			result[k] = v
			canonicalKeys[lowerK] = k
		}
	} else {
		// POSIX: strict case-sensitive merge
		for _, envEntry := range base {
			eq := strings.IndexByte(envEntry, '=')
			if eq <= 0 {
				continue
			}
			k := envEntry[:eq]
			v := envEntry[eq+1:]
			result[k] = v
		}

		for k, v := range explicit {
			result[k] = v
		}
	}

	return result
}
