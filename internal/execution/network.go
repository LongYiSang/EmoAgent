package execution

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
)

type DomainGrant struct {
	ID       string `json:"id,omitempty"`
	Domain   string `json:"domain"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
}

type NetworkRequest struct {
	Domain   string `json:"domain"`
	Protocol string `json:"protocol"`
	Port     int    `json:"port"`
}

type DomainGrantStore interface {
	AllowedDomains(context.Context) ([]DomainGrant, error)
}

type NetworkPolicy struct {
	DefaultMode NetworkMode
	GrantStore  DomainGrantStore
}

func (p NetworkPolicy) Authorize(ctx context.Context, req NetworkRequest) error {
	protocol := strings.ToLower(strings.TrimSpace(req.Protocol))
	domain := normalizeDomain(req.Domain)
	if domain == "" || protocol == "" || req.Port <= 0 {
		return fmt.Errorf("network request requires domain, protocol, and port")
	}
	if p.DefaultMode == NetworkAllow {
		return nil
	}
	if p.GrantStore == nil {
		return fmt.Errorf("network denied: no domain grant")
	}
	grants, err := p.GrantStore.AllowedDomains(ctx)
	if err != nil {
		return err
	}
	for _, grant := range grants {
		if normalizeDomain(grant.Domain) == domain &&
			strings.EqualFold(grant.Protocol, protocol) &&
			grant.Port == req.Port {
			return nil
		}
	}
	return fmt.Errorf("network denied: no matching domain grant")
}

func normalizeDomain(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if host, port, err := net.SplitHostPort(value); err == nil {
		if _, parseErr := strconv.Atoi(port); parseErr == nil {
			return strings.TrimSuffix(strings.TrimSpace(strings.ToLower(host)), ".")
		}
	}
	return strings.TrimSuffix(value, ".")
}
