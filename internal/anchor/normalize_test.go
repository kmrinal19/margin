package anchor

import "testing"

func TestNormalize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, in, want string
	}{
		{"collapse spaces", "a   b\t c", "a b c"},
		{"trim ends", "  hello  ", "hello"},
		{"newlines to space", "a\nb\n c", "a b c"},
		{"already clean", "one two three", "one two three"},
		{"empty", "   ", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := Normalize(c.in); got != c.want {
				t.Errorf("Normalize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestBlockIDFormat(t *testing.T) {
	t.Parallel()
	id := BlockID("The quick brown fox")
	if len(id) != 10 {
		t.Errorf("id length = %d, want 10 (b- + 8 hex)", len(id))
	}
	if id[:2] != "b-" {
		t.Errorf("id = %q, want b- prefix", id)
	}
}

func TestBlockIDWhitespaceInsensitive(t *testing.T) {
	t.Parallel()
	a := BlockID("The quick brown fox")
	b := BlockID("The   quick\tbrown\n  fox")
	if a != b {
		t.Errorf("normalized-equal text produced different ids: %s vs %s", a, b)
	}
}

func TestBlockIDChangesWithText(t *testing.T) {
	t.Parallel()
	if BlockID("The quick brown fox") == BlockID("The quick brown ox") {
		t.Error("different text must produce different ids")
	}
}
