package main

import (
	"strings"
	"testing"
)

func TestValidateCompanyReservationDirectDomainOverlap(t *testing.T) {
	tests := []struct {
		name         string
		direct       domainRule
		wantConflict bool
	}{
		{
			name:         "exact parent domain does not conflict",
			direct:       domainRule{Domain: "example.invalid", IncludeSubdomains: false},
			wantConflict: false,
		},
		{
			name:         "suffix parent domain conflicts",
			direct:       domainRule{Domain: "example.invalid", IncludeSubdomains: true},
			wantConflict: true,
		},
		{
			name:         "exact company apex domain conflicts",
			direct:       domainRule{Domain: "corp.example.invalid", IncludeSubdomains: false},
			wantConflict: true,
		},
		{
			name:         "exact company subdomain conflicts",
			direct:       domainRule{Domain: "internal.corp.example.invalid", IncludeSubdomains: false},
			wantConflict: true,
		},
		{
			name:         "unrelated domain does not conflict",
			direct:       domainRule{Domain: "other.invalid", IncludeSubdomains: true},
			wantConflict: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := routeRequest{
				TransparentRouting: true,
				Gateway:            "198.51.100.3",
				DirectDomains:      []domainRule{tc.direct},
				Company: &companyReservation{
					Networks:    []string{"10.201.0.0/16"},
					Domains:     []string{"corp.example.invalid"},
					GatewayHost: "vpn.example.invalid",
					GatewayPort: 443,
					GatewayIPs:  []string{"198.51.100.4"},
				},
			}

			err := validateCompanyReservation(req, nil, tailscaleBypass{})
			if tc.wantConflict {
				if err == nil || !strings.Contains(err.Error(), "company scope conflicts with direct domain") {
					t.Fatalf("validateCompanyReservation(%+v) = %v, want direct domain conflict error", tc.direct, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateCompanyReservation(%+v) unexpected error: %v", tc.direct, err)
			}
		})
	}
}
