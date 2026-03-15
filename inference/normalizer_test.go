package inference

import (
	"testing"
)

func TestNormalizerCollapseNumericIDs(t *testing.T) {
	n := NewPathNormalizer()
	n.Observe("/users/1")
	n.Observe("/users/42")
	n.Observe("/users/99")
	got := n.Observe("/users/7")
	if got != "/users/{id}" {
		t.Errorf("expected /users/{id}, got %s", got)
	}
}

func TestNormalizerCollapseUUIDs(t *testing.T) {
	n := NewPathNormalizer()
	n.Observe("/orders/550e8400-e29b-41d4-a716-446655440000")
	n.Observe("/orders/6ba7b810-9dad-11d1-80b4-00c04fd430c8")
	got := n.Observe("/orders/6ba7b814-9dad-11d1-80b4-00c04fd430c8")
	if got != "/orders/{id}" {
		t.Errorf("expected /orders/{id}, got %s", got)
	}
}

func TestNormalizerLiteralPreserved(t *testing.T) {
	n := NewPathNormalizer()
	n.Observe("/users/me")
	got := n.Observe("/users/me")
	if got != "/users/me" {
		t.Errorf("expected /users/me, got %s", got)
	}
}

func TestNormalizerNestedParam(t *testing.T) {
	n := NewPathNormalizer()
	n.Observe("/users/1/posts/10")
	n.Observe("/users/2/posts/20")
	n.Observe("/users/3/posts/30")
	got := n.Observe("/users/4/posts/40")
	if got != "/users/{id}/posts/{id}" {
		t.Errorf("expected /users/{id}/posts/{id}, got %s", got)
	}
}

func TestNormalizerSkipsNull(t *testing.T) {
	n := NewPathNormalizer()
	got := n.Observe("/qr-codes/null")
	if got != "" {
		t.Errorf("expected empty string for null segment, got %s", got)
	}
	got = n.Observe("/qr-codes/undefined/download")
	if got != "" {
		t.Errorf("expected empty string for undefined segment, got %s", got)
	}
}

func TestNormalizerURLEncodedPlaceholder(t *testing.T) {
	n := NewPathNormalizer()
	// %7Buuid%7D decodes to {uuid} — client sent template literal
	got := n.Observe("/plans/%7Buuid%7D/summary")
	if got != "/plans/{id}/summary" {
		t.Errorf("expected /plans/{id}/summary, got %s", got)
	}
}

func TestNormalizerSHA1Token(t *testing.T) {
	// 40-char hex token — should collapse on first observation
	n := NewPathNormalizer()
	got := n.Observe("/auth/login/auto/2/9ff90eea5588ff1645144480e52694db4125ddf3")
	if got != "/auth/login/auto/2/{id}" {
		t.Errorf("expected /auth/login/auto/2/{id}, got %s", got)
	}
}

func TestNormalizerHexTokenSingleObservation(t *testing.T) {
	// Token should become {id} even with only 1 observation
	n := NewPathNormalizer()
	got := n.Observe("/email/verify/5/a3f1c2d4e5b6a7f8c9d0e1f2a3b4c5d6")
	if got != "/email/verify/5/{id}" {
		t.Errorf("expected /email/verify/5/{id}, got %s", got)
	}
}
