package githosts

import "testing"

func TestCanonicalDiffRemoteMethodRefs(t *testing.T) {
	t.Parallel()
	if m := canonicalDiffRemoteMethod("refs"); m != refsMethod {
		t.Fatalf("expected %s got %s", refsMethod, m)
	}
	if m := canonicalDiffRemoteMethod("ReFs"); m != refsMethod {
		t.Fatalf("case insensitive expected %s got %s", refsMethod, m)
	}
}

func TestCanonicalDiffRemoteMethodDefault(t *testing.T) {
	t.Parallel()
	if m := canonicalDiffRemoteMethod("clone"); m != cloneMethod {
		t.Fatalf("expected %s got %s", cloneMethod, m)
	}
	if m := canonicalDiffRemoteMethod("bad"); m != cloneMethod {
		t.Fatalf("unexpected %s", m)
	}
}
