package jobs

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// WebhookDestinationPolicy controls outbound webhook destinations.
type WebhookDestinationPolicy struct {
	AllowedHosts map[string]struct{}
	AllowedCIDRs []netip.Prefix
	ResolveNetIP func(context.Context, string) ([]netip.Addr, error)
}

func NewWebhookDestinationPolicy(hosts, cidrs []string) (WebhookDestinationPolicy, error) {
	policy := WebhookDestinationPolicy{AllowedHosts: map[string]struct{}{}, ResolveNetIP: defaultWebhookResolveNetIP}
	for _, host := range hosts {
		host = strings.ToLower(strings.TrimSpace(host))
		if host != "" {
			policy.AllowedHosts[host] = struct{}{}
		}
	}
	for _, raw := range cidrs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return WebhookDestinationPolicy{}, fmt.Errorf("parse webhook allowed CIDR %q: %w", raw, err)
		}
		policy.AllowedCIDRs = append(policy.AllowedCIDRs, prefix)
	}
	return policy, nil
}

func (p WebhookDestinationPolicy) ValidateHost(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	_, explicitlyAllowed := p.AllowedHosts[host]
	if ip, err := netip.ParseAddr(host); err == nil {
		if explicitlyAllowed {
			return []netip.Addr{ip.Unmap()}, nil
		}
		if p.allowedIP(ip) {
			return []netip.Addr{ip}, nil
		}
		return nil, fmt.Errorf("webhook destination IP %s is not allowed", ip)
	}
	resolver := p.ResolveNetIP
	if resolver == nil {
		resolver = defaultWebhookResolveNetIP
	}
	addresses, err := resolver(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve webhook destination %s: %w", host, err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("webhook destination %s resolved to no addresses", host)
	}
	if explicitlyAllowed {
		return addresses, nil
	}
	for _, address := range addresses {
		if !p.allowedIP(address) {
			return nil, fmt.Errorf("webhook destination %s resolved to disallowed IP %s", host, address)
		}
	}
	return addresses, nil
}

func (p WebhookDestinationPolicy) allowedIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	for _, prefix := range p.AllowedCIDRs {
		if prefix.Contains(ip) {
			return true
		}
	}
	return !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified() && !ip.IsPrivate()
}

func defaultWebhookResolveNetIP(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}
