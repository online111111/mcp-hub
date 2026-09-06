package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
)

var (
	// serverIDRegex matches valid server IDs: ^[a-z][a-z0-9_-]{0,31}$
	serverIDRegex = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

	// ErrInvalidServerID indicates an invalid server ID.
	ErrInvalidServerID = errors.New("invalid server ID: must match ^[a-z][a-z0-9_-]{0,31}$")

	// ErrEmptyToolName indicates an empty original tool name.
	ErrEmptyToolName = errors.New("tool name cannot be empty")

	// ErrNameCollision indicates a public tool name collision.
	ErrNameCollision = errors.New("public tool name collision")
)

// ValidateServerID checks whether serverID conforms to the spec: ^[a-z][a-z0-9_-]{0,31}$
func ValidateServerID(serverID string) error {
	if !serverIDRegex.MatchString(serverID) {
		return fmt.Errorf("%w: %q", ErrInvalidServerID, serverID)
	}
	return nil
}

// IsAllowedPublicNameChar reports whether byte c is in [A-Za-z0-9_-].
func IsAllowedPublicNameChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_' ||
		c == '-'
}

// IsValidPublicToolName checks whether name contains only [A-Za-z0-9_-] and length <= 64.
func IsValidPublicToolName(name string) bool {
	if len(name) == 0 || len(name) > MaxPublicToolNameLength {
		return false
	}
	for i := 0; i < len(name); i++ {
		if !IsAllowedPublicNameChar(name[i]) {
			return false
		}
	}
	return true
}

// ComputeHashedAlias computes the stable SHA-256 alias:
// serverID + "__h" + hex(SHA256(serverID + NUL + originalName))[:24]
func ComputeHashedAlias(serverID, originalName string) string {
	h := sha256.New()
	h.Write([]byte(serverID))
	h.Write([]byte{0}) // NUL byte (\x00)
	h.Write([]byte(originalName))
	sum := h.Sum(nil)
	hexStr := hex.EncodeToString(sum)
	return serverID + "__h" + hexStr[:24]
}

// ResolveDirectName attempts to form <serverID>__<originalName>.
// It returns (directName, true) if valid ([A-Za-z0-9_-] and length <= 64),
// or ("", false) if invalid characters or length exceeds 64.
func ResolveDirectName(serverID, originalName string) (string, bool) {
	if originalName == "" {
		return "", false
	}
	direct := serverID + "__" + originalName
	if !IsValidPublicToolName(direct) {
		return "", false
	}
	return direct, true
}

// ResolveCandidateName determines the intended public name (direct name if valid,
// otherwise stable hashed alias) for an original tool name on a server.
func ResolveCandidateName(serverID, originalName string) (name string, isAlias bool) {
	if direct, ok := ResolveDirectName(serverID, originalName); ok {
		return direct, false
	}
	return ComputeHashedAlias(serverID, originalName), true
}

// ToolNameCandidate holds the naming decision for a tool candidate.
type ToolNameCandidate struct {
	Index        int
	OriginalName string
	PublicName   string
	IsAlias      bool
	DirectValid  bool
	Err          error
}

// ResolveNamesWithPriority resolves public tool names using two-pass allocation:
// Pass 1: Allocate valid direct names (<serverID>__<originalName>).
// Pass 2: Allocate stable hashed aliases for remaining tools.
// If any name is already claimed (in existingClaimed or earlier in the allocation),
// it is rejected without overwriting, and marked with ErrNameCollision.
func ResolveNamesWithPriority(serverID string, originalNames []string, existingClaimed map[string]bool) []ToolNameCandidate {
	results := make([]ToolNameCandidate, len(originalNames))
	claimed := make(map[string]int) // publicName -> candidate index

	// Populate already claimed names from other servers/sources
	for k := range existingClaimed {
		claimed[k] = -1 // -1 means claimed externally
	}

	// Initialize candidates
	for i, orig := range originalNames {
		results[i] = ToolNameCandidate{
			Index:        i,
			OriginalName: orig,
		}
		if orig == "" {
			results[i].Err = ErrEmptyToolName
			continue
		}
		if direct, ok := ResolveDirectName(serverID, orig); ok {
			results[i].DirectValid = true
			results[i].PublicName = direct
		} else {
			results[i].DirectValid = false
			results[i].PublicName = ComputeHashedAlias(serverID, orig)
			results[i].IsAlias = true
		}
	}

	// Pass 1: Allocate valid direct names
	for i := range results {
		c := &results[i]
		if c.Err != nil || !c.DirectValid {
			continue
		}
		if _, exists := claimed[c.PublicName]; exists {
			c.Err = fmt.Errorf("%w: direct name %q already claimed", ErrNameCollision, c.PublicName)
			continue
		}
		claimed[c.PublicName] = i
	}

	// Pass 2: Allocate hashed aliases
	for i := range results {
		c := &results[i]
		if c.Err != nil || c.DirectValid {
			continue
		}
		if _, exists := claimed[c.PublicName]; exists {
			c.Err = fmt.Errorf("%w: hash alias %q already claimed", ErrNameCollision, c.PublicName)
			continue
		}
		claimed[c.PublicName] = i
	}

	return results
}
