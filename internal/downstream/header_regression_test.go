package downstream

import (
	"errors"
	"testing"
)

func TestValidateHeadersRejectsHopByHop(t *testing.T) {
	for _, name := range []string{"Transfer-Encoding", "Upgrade", "Trailer", "TE", "Keep-Alive", "Proxy-Authorization", "Proxy-Connection"} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateHeaders(map[string]string{name: "value"}); !errors.Is(err, ErrForbiddenHeader) {
				t.Fatalf("accepted hop-by-hop header %q: %v", name, err)
			}
		})
	}
}
