//go:build !windows

package process

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"
)

func TestExplicitEmptyEnvironmentDoesNotInheritParent(t *testing.T) {
	t.Setenv("AUDIT_PARENT_SECRET", "must-not-be-inherited")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	child, err := Start(ctx, Spec{Command: "/usr/bin/env", Env: []string{}})
	if err != nil {
		t.Fatal(err)
	}
	defer child.Close()
	output, err := io.ReadAll(child.Reader())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output, []byte("AUDIT_PARENT_SECRET=")) {
		t.Fatal("explicitly empty child environment inherited a parent secret")
	}
}
