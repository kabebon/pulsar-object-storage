package service

import (
	"testing"
)

func TestValidateBucketName(t *testing.T) {
	t.Parallel()
	good := []string{
		"my-bucket", "bucket.2026", "abc", "a1-b2.c3",
		"123456789012345678901234567890123456789012345678901234567890123", // 63 chars
	}
	for _, n := range good {
		if err := validateBucketName(n); err != nil {
			t.Errorf("expected %q to be valid, got %v", n, err)
		}
	}
	bad := []string{
		"", "ab", "a", // too short
		"UPPER", "under_score", "has space", "слон", // invalid chars
		"-leading", "trailing-", ".leading", "trailing.", // bad edges
		"two..dots", // consecutive dots
		string(make([]byte, 64)), // too long (zeros, also invalid chars)
	}
	for _, n := range bad {
		if err := validateBucketName(n); err == nil {
			t.Errorf("expected %q to be invalid", n)
		}
	}
}

func TestValidateObjectKey(t *testing.T) {
	t.Parallel()
	if err := validateObjectKey("path/to/file.txt"); err != nil {
		t.Error(err)
	}
	if err := validateObjectKey(""); err == nil {
		t.Error("empty key should be rejected")
	}
	if err := validateObjectKey("../escape"); err == nil {
		t.Error("path traversal should be rejected")
	}
	long := make([]byte, 1025)
	for i := range long {
		long[i] = 'a'
	}
	if err := validateObjectKey(string(long)); err == nil {
		t.Error("overlong key should be rejected")
	}
}
