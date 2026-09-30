package browser

import "testing"

func TestAllowedURL(t *testing.T) {
	if !allowedURL("http://localhost:3000") || !allowedURL("https://example.com") {
		t.Fatal("expected URL to be allowed")
	}
	for _, value := range []string{"file:///etc/passwd", "javascript:alert(1)", "ftp://example.com"} {
		if allowedURL(value) {
			t.Fatalf("expected %s to be denied", value)
		}
	}
}
