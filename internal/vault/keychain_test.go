package vault

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestKeychainRoundTrip uses the real login keychain with a throwaway item. Run with
// AIOFCAGE_KEYCHAIN_TEST=1.
func TestKeychainRoundTrip(t *testing.T) {
	if os.Getenv("AIOFCAGE_KEYCHAIN_TEST") == "" {
		t.Skip("set AIOFCAGE_KEYCHAIN_TEST=1 to use the real keychain")
	}
	k, ctx, item := NewKeychain(), context.Background(), "aiofcage-test-vault-roundtrip"
	t.Cleanup(func() { _ = k.Delete(ctx, item) })
	long := `{"access_token":"` + strings.Repeat("a1b2", 2500) + `","note":"带空格 和 'quotes' \\ "}`
	for _, value := range []string{"plain-api-key-123", `p@ss word "quoted" 'single' \back`, long, "aiofcage-looks-like-a-prefix", "short-again"} {
		if err := k.Put(ctx, item, []byte(value)); err != nil {
			t.Fatalf("put %d bytes: %v", len(value), err)
		}
		got, err := k.Read(ctx, item)
		if err != nil || string(got) != value {
			t.Fatalf("round trip of %d bytes failed: %v", len(value), err)
		}
	}
	if n := k.partCount(ctx, item); n != 1 {
		t.Fatalf("parts of a shrunk value must be cleaned up: %d", n)
	}
	if _, err := k.readItem(ctx, partName(item, 2)); err == nil {
		t.Fatal("stale part left behind")
	}
	if err := k.Delete(ctx, item); err != nil {
		t.Fatal(err)
	}
	if _, err := k.Read(ctx, item); err != ErrNotFound {
		t.Fatalf("deleted item still readable: %v", err)
	}
}
