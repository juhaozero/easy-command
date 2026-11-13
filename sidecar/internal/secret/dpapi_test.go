package secret

import (
	"bytes"
	"errors"
	"testing"
)

func TestDPAPIRoundTrip(t *testing.T) {
	plaintext := []byte("DPAPI test 密码")
	ciphertext, err := Protect(plaintext)
	if err != nil {
		t.Fatal(err)
	}
	if len(ciphertext) == 0 || bytes.Contains(ciphertext, plaintext) {
		t.Fatal("invalid ciphertext")
	}
	got, err := Unprotect(ciphertext)
	if err != nil || !bytes.Equal(got, plaintext) {
		t.Fatal("DPAPI round trip failed")
	}
	clear(got)
	if _, err := Unprotect([]byte("invalid DPAPI blob")); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("bad blob: %v", err)
	}
	if _, err := Unprotect(nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty blob: %v", err)
	}
}
