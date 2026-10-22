package flags

import (
	"fmt"
	"testing"
)

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

func TestSetRolloutValidation(t *testing.T) {
	s := NewStore()
	_ = s.Create("r", true)

	for _, p := range []int{-1, 101} {
		if err := s.SetRollout("r", p); err != ErrInvalidPercentage {
			t.Fatalf("SetRollout(%d): expected ErrInvalidPercentage, got %v", p, err)
		}
	}
	if err := s.SetRollout("missing", 50); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestRolloutExtremes(t *testing.T) {
	s := NewStore()
	_ = s.Create("r", true)

	_ = s.SetRollout("r", 0)
	_ = s.SetRollout("r", 100)
	for i := 0; i < 100; i++ {
		if !s.IsEnabledFor("r", fmt.Sprintf("user-%d", i)) {
			t.Fatal("100% rollout should be on for everyone")
		}
	}

	_ = s.SetRollout("r", 0)
	for i := 0; i < 100; i++ {
		if s.IsEnabledFor("r", fmt.Sprintf("user-%d", i)) {
			t.Fatal("0% rollout should be off for everyone")
		}
	}
}

func TestRolloutDeterministic(t *testing.T) {
	s := NewStore()
	_ = s.Create("r", true)
	_ = s.SetRollout("r", 50)

	for i := 0; i < 100; i++ {
		u := fmt.Sprintf("user-%d", i)
		first := s.IsEnabledFor("r", u)
		for j := 0; j < 5; j++ {
			if s.IsEnabledFor("r", u) != first {
				t.Fatalf("result for %s changed between calls", u)
			}
		}
	}
}

func TestRolloutDistribution(t *testing.T) {
	s := NewStore()
	_ = s.Create("r", true)
	_ = s.SetRollout("r", 30)

	const n = 10000
	on := 0
	for i := 0; i < n; i++ {
		if s.IsEnabledFor("r", fmt.Sprintf("user-%d", i)) {
			on++
		}
	}
	if on < 2700 || on > 3300 {
		t.Fatalf("expected ~30%% enabled, got %d/%d", on, n)
	}
}

func TestRolloutMonotonic(t *testing.T) {
	s := NewStore()
	_ = s.Create("r", true)

	_ = s.SetRollout("r", 20)
	var enabled []string
	for i := 0; i < 1000; i++ {
		u := fmt.Sprintf("user-%d", i)
		if s.IsEnabledFor("r", u) {
			enabled = append(enabled, u)
		}
	}
	_ = s.SetRollout("r", 60)
	for _, u := range enabled {
		if !s.IsEnabledFor("r", u) {
			t.Fatalf("%s lost access when rollout increased", u)
		}
	}
}

func TestIsEnabledForDisabledOrMissing(t *testing.T) {
	s := NewStore()
	_ = s.Create("off", false)

	if s.IsEnabledFor("off", "u1") {
		t.Fatal("disabled flag should be off")
	}
	if s.IsEnabledFor("missing", "u1") {
		t.Fatal("missing flag should be off")
	}
}

func TestIsEnabledPartialRolloutIsFalse(t *testing.T) {
	s := NewStore()
	_ = s.Create("r", true)
	_ = s.SetRollout("r", 50)

	if s.IsEnabled("r") {
		t.Fatal("IsEnabled should be false for a partial rollout")
	}
}
