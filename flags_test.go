package flags

import "testing"

func TestCreateAndGet(t *testing.T) {
	s := NewStore()

	if err := s.Create("new-checkout", true); err != nil {
		t.Fatalf("unexpected error creating flag: %v", err)
	}

	f, err := s.Get("new-checkout")
	if err != nil {
		t.Fatalf("unexpected error getting flag: %v", err)
	}
	if !f.Enabled {
		t.Fatal("expected flag to be enabled")
	}
}

func TestCreateDuplicateFails(t *testing.T) {
	s := NewStore()
	_ = s.Create("dup", false)

	if err := s.Create("dup", true); err == nil {
		t.Fatal("expected error creating duplicate flag")
	}
}

func TestGetMissingReturnsNotFound(t *testing.T) {
	s := NewStore()

	if _, err := s.Get("missing"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSetUpdatesValue(t *testing.T) {
	s := NewStore()
	_ = s.Create("beta", false)

	if err := s.Set("beta", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.IsEnabled("beta") {
		t.Fatal("expected flag to be enabled after Set")
	}
}

func TestSetMissingReturnsNotFound(t *testing.T) {
	s := NewStore()
	if err := s.Set("missing", true); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestDelete(t *testing.T) {
	s := NewStore()
	_ = s.Create("temp", true)

	if err := s.Delete("temp"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := s.Get("temp"); err != ErrNotFound {
		t.Fatal("expected flag to be gone after delete")
	}
}

func TestDeleteMissingReturnsNotFound(t *testing.T) {
	s := NewStore()
	if err := s.Delete("missing"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestList(t *testing.T) {
	s := NewStore()
	_ = s.Create("a", true)
	_ = s.Create("b", false)

	got := s.List()
	if len(got) != 2 {
		t.Fatalf("expected 2 flags, got %d", len(got))
	}
}

func TestIsEnabledUnknownFlagIsFalse(t *testing.T) {
	s := NewStore()
	if s.IsEnabled("nope") {
		t.Fatal("expected unknown flag to be treated as disabled")
	}
}
