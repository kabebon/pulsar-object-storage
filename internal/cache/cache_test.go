package cache

import "testing"

func TestEncodeValue(t *testing.T) {
	t.Parallel()
	// Primitive types pass through unchanged.
	for _, v := range []any{"hello", []byte("bytes"), 42, int64(7), 3.14, true, nil} {
		out, err := encodeValue(v)
		if err != nil {
			t.Errorf("encodeValue(%T): %v", v, err)
		}
		_ = out
	}
	// Structs become JSON.
	type sample struct{ A string }
	out, err := encodeValue(sample{A: "x"})
	if err != nil {
		t.Fatalf("encodeValue struct: %v", err)
	}
	if b, ok := out.([]byte); !ok || string(b) != `{"A":"x"}` {
		t.Errorf("unexpected encoding: %v", out)
	}
}

func TestDecodeJSON(t *testing.T) {
	t.Parallel()
	var got SessionData
	if err := decodeJSON([]byte(`{"user_id":"u1","email":"a@b.c"}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.UserID != "u1" || got.Email != "a@b.c" {
		t.Errorf("decoded = %+v", got)
	}
}

func TestGenerateToken(t *testing.T) {
	t.Parallel()
	a, err := generateToken(32)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := generateToken(32)
	if a == b {
		t.Fatal("identical tokens")
	}
}

func TestToInt64(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   any
		want int64
	}{
		{int64(5), 5}, {int(5), 5}, {float64(5), 5}, {"x", 0},
	}
	for _, c := range cases {
		if got := toInt64(c.in); got != c.want {
			t.Errorf("toInt64(%v) = %d, want %d", c.in, got, c.want)
		}
	}
}
