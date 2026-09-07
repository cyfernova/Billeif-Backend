package app

import "testing"

func TestRuntimeRepositoriesProvideDurableCapabilityProviderHealth(t *testing.T) {
	repositories := initRepositories(nil)
	if repositories.CapabilityProviderHealth == nil {
		t.Fatal("runtime must inject the durable capability provider-health repository")
	}
}
