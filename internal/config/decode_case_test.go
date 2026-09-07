package config

import "testing"

func TestDecodeStrictRejectsIncorrectFieldCase(t *testing.T) {
	data := []byte(`{
		"version": 1,
		"hub": {"PublicMode": false},
		"mcpServers": {}
	}`)
	var cfg Config
	if err := DecodeStrict(data, &cfg); err == nil {
		t.Fatal("expected incorrectly cased field name to be rejected")
	}
}

func TestDecodeStrictAcceptsExactNestedMapFields(t *testing.T) {
	data := []byte(`{
		"version": 1,
		"hub": {"publicMode": false},
		"mcpServers": {
			"local": {"type":"stdio","command":"node","env":{"MixedCaseKey":"ok"}}
		}
	}`)
	var cfg Config
	if err := DecodeStrict(data, &cfg); err != nil {
		t.Fatalf("DecodeStrict rejected valid map keys: %v", err)
	}
}
