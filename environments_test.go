package flags

import "testing"

func TestEnvironmentsDefaultNames(t *testing.T) {
	got := NewEnvironments().Names()
	want := []string{"dev", "prod", "staging"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestEnvironmentsAreIndependent(t *testing.T) {
	e := NewEnvironments()
	dev, _ := e.Env(EnvDev)
	prod, _ := e.Env(EnvProd)

	_ = dev.Create("new-ui", true)
	_ = prod.Create("new-ui", false)

	if !dev.IsEnabled("new-ui") || prod.IsEnabled("new-ui") {
		t.Fatal("dev and prod should hold independent state")
	}

	_ = dev.Delete("new-ui")
	if _, err := prod.Get("new-ui"); err != nil {
		t.Fatalf("deleting in dev must not affect prod: %v", err)
	}
}

func TestUnknownEnvironment(t *testing.T) {
	e := NewEnvironments()
	if _, err := e.Env("qa"); err != ErrUnknownEnvironment {
		t.Fatalf("got %v, want ErrUnknownEnvironment", err)
	}
	if err := e.Promote("x", "qa", EnvProd); err != ErrUnknownEnvironment {
		t.Fatalf("got %v, want ErrUnknownEnvironment", err)
	}
	if err := e.Promote("x", EnvDev, "qa"); err != ErrUnknownEnvironment {
		t.Fatalf("got %v, want ErrUnknownEnvironment", err)
	}
}

func TestPromoteCopiesConfigAndIsolates(t *testing.T) {
	e := NewEnvironments("dev", "prod")
	dev, _ := e.Env("dev")
	prod, _ := e.Env("prod")

	_ = dev.Create("beta", true)
	_ = dev.SetRollout("beta", 40)
	_ = dev.AddRule("beta", Rule{Attribute: "email", Operator: OpEndsWith, Values: []string{"@acme.com"}})

	if err := e.Promote("beta", "dev", "prod"); err != nil {
		t.Fatalf("promote: %v", err)
	}
	f, err := prod.Get("beta")
	if err != nil || !f.Enabled || f.Rollout != 40 || len(f.Rules) != 1 {
		t.Fatalf("unexpected promoted flag %+v (err %v)", f, err)
	}

	// Later changes in dev must not leak into prod.
	_ = dev.ClearRules("beta")
	_ = dev.Set("beta", false)
	f, _ = prod.Get("beta")
	if !f.Enabled || len(f.Rules) != 1 {
		t.Fatalf("prod changed after dev edit: %+v", f)
	}
	if !prod.Evaluate("beta", Context{UserID: "u", Attributes: map[string]string{"email": "a@acme.com"}}) {
		t.Fatal("promoted rule should still match in prod")
	}
}

func TestPromoteOverwritesAndMissingFlag(t *testing.T) {
	e := NewEnvironments()
	dev, _ := e.Env(EnvDev)
	stg, _ := e.Env(EnvStaging)

	if err := e.Promote("nope", EnvDev, EnvStaging); err != ErrNotFound {
		t.Fatalf("got %v, want ErrNotFound", err)
	}

	_ = dev.Create("f", true)
	_ = stg.Create("f", false)
	if err := e.Promote("f", EnvDev, EnvStaging); err != nil {
		t.Fatal(err)
	}
	if !stg.IsEnabled("f") {
		t.Fatal("promote should overwrite the target flag")
	}
}
