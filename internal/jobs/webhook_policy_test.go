package jobs

import (
	"context"
	"net/netip"
	"testing"
)

func TestWebhookDestinationPolicyRejectsUnsafeAddresses(t *testing.T) {
	policy, err := NewWebhookDestinationPolicy(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"127.0.0.1", "::1", "10.0.0.1", "169.254.169.254", "224.0.0.1", "0.0.0.0"} {
		if _, err := policy.ValidateHost(context.Background(), host); err == nil {
			t.Fatalf("expected %s to be rejected", host)
		}
	}
}

func TestWebhookDestinationPolicyRejectsMixedDNSAndAllowsExplicitInternalSinks(t *testing.T) {
	policy, err := NewWebhookDestinationPolicy(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	policy.ResolveNetIP = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("203.0.113.10"), netip.MustParseAddr("10.0.0.10")}, nil
	}
	if _, err := policy.ValidateHost(context.Background(), "mixed.example"); err == nil {
		t.Fatal("expected mixed public/private DNS result to be rejected")
	}
	allowed, err := NewWebhookDestinationPolicy([]string{"sink.internal"}, []string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	allowed.ResolveNetIP = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("10.0.0.10")}, nil
	}
	addresses, err := allowed.ValidateHost(context.Background(), "sink.internal")
	if err != nil {
		t.Fatalf("expected explicitly allowed host: %v", err)
	}
	if len(addresses) != 1 || addresses[0] != netip.MustParseAddr("10.0.0.10") {
		t.Fatalf("expected resolved allowed host address, got %v", addresses)
	}
	if _, err := allowed.ValidateHost(context.Background(), "10.3.4.5"); err != nil {
		t.Fatalf("expected explicitly allowed CIDR: %v", err)
	}
}
