package navguard

import (
	"net"
	"net/netip"
)

// Validator carries pre-parsed security configuration for navigation target
// validation. Handlers construct one per call because trusted CIDRs come from
// the per-request (target-scoped) effective config; remote-IP validation uses
// the standalone ValidateRemoteIP function. It is safe for concurrent use when
// fields are not mutated after construction.
type Validator struct {
	TrustedResolveCIDRs []*net.IPNet
	// ProxyResolvesNames is set when every navigation goes through a proxy that
	// resolves hostnames itself (Chrome sends them unresolved to a SOCKS5 proxy). A
	// local lookup then checks addresses the browser never connects to, leaks each
	// visited name outside the proxy, and fails outright for names only the proxy
	// can resolve, such as .onion.
	ProxyResolvesNames bool
}

// ValidatedTarget is the result of ValidateTarget. It signals which runtime
// checks may be relaxed for this navigation.
type ValidatedTarget struct {
	AllowInternal     bool
	TrustedResolvedIP []netip.Addr
}
