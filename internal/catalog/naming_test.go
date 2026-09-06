package catalog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func TestValidateServerID(t *testing.T) {
	validIDs := []string{
		"a",
		"filesystem",
		"git-tool",
		"srv_1",
		"server-123_abc",
		"a1234567890123456789012345678901", // 32 chars starting with letter
	}
	for _, id := range validIDs {
		if err := ValidateServerID(id); err != nil {
			t.Errorf("expected valid server ID %q, got error: %v", id, err)
		}
	}

	invalidIDs := []string{
		"",                                  // empty
		"A",                                 // uppercase
		"Filesystem",                        // uppercase
		"1server",                           // starts with digit
		"-server",                           // starts with hyphen
		"_server",                           // starts with underscore
		"server name",                       // space
		"server@tool",                       // special char
		"a12345678901234567890123456789012", // 33 chars (exceeds 32)
	}
	for _, id := range invalidIDs {
		if err := ValidateServerID(id); err == nil {
			t.Errorf("expected invalid server ID %q, got nil error", id)
		}
	}
}

func TestDirectNaming(t *testing.T) {
	serverID := "filesystem"
	origName := "read_file"

	direct, ok := ResolveDirectName(serverID, origName)
	if !ok {
		t.Fatalf("expected valid direct name for %s and %s", serverID, origName)
	}
	if direct != "filesystem__read_file" {
		t.Errorf("expected filesystem__read_file, got %s", direct)
	}

	// Exactly 64 chars
	// "filesystem__" is 12 chars, so original name with 52 chars = 64 chars total
	orig52 := strings.Repeat("a", 52)
	direct64, ok := ResolveDirectName(serverID, orig52)
	if !ok || len(direct64) != 64 {
		t.Fatalf("expected valid 64-char direct name, got len=%d, ok=%v", len(direct64), ok)
	}

	// 65 chars (12 + 53 = 65) -> invalid direct name
	orig53 := strings.Repeat("a", 53)
	_, ok = ResolveDirectName(serverID, orig53)
	if ok {
		t.Errorf("expected 65-char direct name to fail, but succeeded")
	}

	// Invalid characters in original name
	invalidNames := []string{
		"read file", // space
		"read.file", // dot
		"read:file", // colon
		"read/file", // slash
		"read@file", // at symbol
		"读取文件",      // unicode
		"",          // empty
	}
	for _, inv := range invalidNames {
		if _, ok := ResolveDirectName(serverID, inv); ok {
			t.Errorf("expected direct naming to fail for invalid original name %q", inv)
		}
	}
}

func TestHashedAliasFormula(t *testing.T) {
	serverID := "filesystem"
	origName := "read file with spaces"

	alias := ComputeHashedAlias(serverID, origName)

	// Verify formula: serverID + "__h" + hex(SHA256(serverID + NUL + originalName))[:24]
	h := sha256.New()
	h.Write([]byte(serverID))
	h.Write([]byte{0})
	h.Write([]byte(origName))
	expectedHex := hex.EncodeToString(h.Sum(nil))[:24]
	expected := "filesystem__h" + expectedHex

	if alias != expected {
		t.Fatalf("alias mismatch: expected %q, got %q", expected, alias)
	}

	// Max length check: serverID (<=32) + "__h" (3) + 24 = 59 <= 64
	if len(alias) > 64 {
		t.Errorf("alias length %d exceeds 64", len(alias))
	}
	if !IsValidPublicToolName(alias) {
		t.Errorf("alias %q must be valid public tool name", alias)
	}
}

func TestResolveNamesPriorityAndCollision(t *testing.T) {
	serverID := "srv"

	// 1. Two tools with valid direct names
	names := []string{"tool_a", "tool_b"}
	candidates := ResolveNamesWithPriority(serverID, names, nil)
	if len(candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(candidates))
	}
	if candidates[0].PublicName != "srv__tool_a" || candidates[0].IsAlias {
		t.Errorf("unexpected candidate 0: %+v", candidates[0])
	}
	if candidates[1].PublicName != "srv__tool_b" || candidates[1].IsAlias {
		t.Errorf("unexpected candidate 1: %+v", candidates[1])
	}

	// 2. Duplicate tool name in same server -> collision rejection without overwriting
	namesDup := []string{"dup_tool", "dup_tool"}
	candDup := ResolveNamesWithPriority(serverID, namesDup, nil)
	if candDup[0].Err != nil {
		t.Errorf("candidate 0 should succeed: %v", candDup[0].Err)
	}
	if candDup[1].Err == nil || !errors.Is(candDup[1].Err, ErrNameCollision) {
		t.Errorf("candidate 1 should fail with ErrNameCollision, got %v", candDup[1].Err)
	}

	// 3. Priority test: direct name claims name, preventing hash alias collision
	// Let's create an original name that requires hash alias
	invalidOrig := "needs alias!"
	aliasTarget := ComputeHashedAlias(serverID, invalidOrig)
	// Extract the part after "srv__"
	directCollidingOrig := strings.TrimPrefix(aliasTarget, serverID+"__")

	// Pass both: one has direct name that matches aliasTarget, one has invalidOrig that hashes to aliasTarget
	// Because Pass 1 allocates direct names first, the direct tool gets it, and the hash tool gets a collision!
	priorityNames := []string{invalidOrig, directCollidingOrig}
	candPriority := ResolveNamesWithPriority(serverID, priorityNames, nil)

	// candPriority[0] had invalidOrig -> evaluated in Pass 2 -> collided with directCollidingOrig!
	// candPriority[1] had directCollidingOrig -> evaluated in Pass 1 -> claimed direct name!
	if candPriority[1].Err != nil {
		t.Errorf("direct name candidate should succeed: %v", candPriority[1].Err)
	}
	if candPriority[0].Err == nil || !errors.Is(candPriority[0].Err, ErrNameCollision) {
		t.Errorf("hash alias candidate should have collided and failed with ErrNameCollision, got %v", candPriority[0].Err)
	}

	// 4. Pre-claimed external names
	existing := map[string]bool{
		"srv__external_tool": true,
	}
	candExt := ResolveNamesWithPriority(serverID, []string{"external_tool"}, existing)
	if candExt[0].Err == nil || !errors.Is(candExt[0].Err, ErrNameCollision) {
		t.Errorf("expected collision with external tool, got %v", candExt[0].Err)
	}
}
