package main

import (
	"fmt"

	flags "github.com/saif177/feature-flag-service"
)

func main() {
	store := flags.NewStore()

	_ = store.Create("new-checkout", false)
	_ = store.Create("dark-mode", true)

	fmt.Println("dark-mode enabled:", store.IsEnabled("dark-mode"))
	fmt.Println("new-checkout enabled:", store.IsEnabled("new-checkout"))

	_ = store.Set("new-checkout", true)
	fmt.Println("new-checkout enabled after rollout:", store.IsEnabled("new-checkout"))

	fmt.Println("all flags:")
	for _, f := range store.List() {
		fmt.Printf("  %s: %v\n", f.Key, f.Enabled)
	}
}
