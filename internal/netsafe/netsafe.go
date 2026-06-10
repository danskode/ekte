// Package netsafe samler SSRF-relaterede netværkstjek der deles på tværs af
// pakker (internal/tools og internal/provider), så IP-blokeringslisterne ikke
// kan divergere.
package netsafe

import "net"

// IsPrivateIP returnerer true for loopback, link-local, private og ULA-adresser.
// Bruger Go 1.17+ ip.IsPrivate() der korrekt håndterer IPv6 ULA (fc00::/7).
func IsPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}
	if ip.IsPrivate() { // dækker 10/8, 172.16/12, 192.168/16, fc00::/7
		return true
	}
	if ip4 := ip.To4(); ip4 != nil {
		// 169.254.0.0/16 cloud metadata (dækket af IsLinkLocalUnicast, men eksplicit)
		if ip4[0] == 169 && ip4[1] == 254 {
			return true
		}
		// 0.0.0.0/8 — net.IP.IsPrivate() og IsLoopback() returnerer begge false for disse
		if ip4[0] == 0 {
			return true
		}
	}
	return false
}
