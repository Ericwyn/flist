package util

import "testing"

func TestGenerateTokenUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		tok, err := GenerateToken()
		if err != nil {
			t.Fatal(err)
		}
		if len(tok) < 40 {
			t.Errorf("token too short: %d", len(tok))
		}
		if seen[tok] {
			t.Fatalf("duplicate token generated: %s", tok)
		}
		seen[tok] = true
	}
}

func TestHashTokenStable(t *testing.T) {
	tok := "abc123"
	h1 := HashToken(tok)
	h2 := HashToken(tok)
	if h1 != h2 {
		t.Errorf("hash not stable: %s != %s", h1, h2)
	}
	if len(h1) != 64 {
		t.Errorf("expected 64 hex chars, got %d", len(h1))
	}
	if HashToken("other") == h1 {
		t.Error("different inputs produced same hash")
	}
}
