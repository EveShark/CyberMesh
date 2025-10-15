package utils

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	multiaddr "github.com/multiformats/go-multiaddr"
)

// Network security errors
var (
	ErrInvalidAddress      = errors.New("net: invalid address")
	ErrAddressNotAllowed   = errors.New("net: address not allowed")
	ErrPrivateIPNotAllowed = errors.New("net: private IP not allowed")
	ErrDNSRebinding        = errors.New("net: potential DNS rebinding attack")
	ErrPortOutOfRange      = errors.New("net: port out of range")
	ErrInvalidMultiaddr    = errors.New("net: invalid multiaddr")
)

// IPAllowlistConfig configures IP allowlist behavior
type IPAllowlistConfig struct {
	// Allowed networks
	AllowedCIDRs []string

	// Security options
	AllowPrivate   bool
	AllowLoopback  bool
	AllowLinkLocal bool
	AllowMulticast bool

	// DNS security
	DisableDNS       bool
	DNSCacheTTL      time.Duration
	ValidateDNSOnUse bool

	// Logging
	Logger *Logger
}

// DefaultIPAllowlistConfig returns secure defaults
func DefaultIPAllowlistConfig() *IPAllowlistConfig {
	return &IPAllowlistConfig{
		AllowPrivate:     false,
		AllowLoopback:    true,
		AllowLinkLocal:   false,
		AllowMulticast:   false,
		DisableDNS:       true, // Disable DNS to prevent rebinding
		DNSCacheTTL:      5 * time.Minute,
		ValidateDNSOnUse: true,
	}
}

// IPAllowlist provides secure IP filtering with DNS rebinding protection
type IPAllowlist struct {
	config   *IPAllowlistConfig
	networks []net.IPNet

	// DNS cache for rebinding protection
	dnsCache     map[string]*dnsCacheEntry
	dnsCacheMu   sync.RWMutex
	maxCacheSize int

	// Metrics
	allowedCount uint64
	deniedCount  uint64
	metricsMu    sync.RWMutex
}

type dnsCacheEntry struct {
	ips       []net.IP
	timestamp time.Time
	hostname  string
}

// NewIPAllowlist creates a new IP allowlist
func NewIPAllowlist(config *IPAllowlistConfig) (*IPAllowlist, error) {
	if config == nil {
		config = DefaultIPAllowlistConfig()
	}

	al := &IPAllowlist{
		config:       config,
		dnsCache:     make(map[string]*dnsCacheEntry),
		maxCacheSize: 1000, // reasonable limit
	}

	// Parse CIDR blocks
	if len(config.AllowedCIDRs) > 0 {
		al.networks = ParseCIDRs(config.AllowedCIDRs)
	}

	return al, nil
}

// IsAllowed checks if an IP is allowed
func (al *IPAllowlist) IsAllowed(ip net.IP) bool {
	// Check IP type restrictions
	if !al.checkIPType(ip) {
		al.recordDenied()
		return false
	}

	// If no allowlist configured, allow by default (after type checks)
	if len(al.networks) == 0 {
		al.recordAllowed()
		return true
	}

	// Check against allowlist
	for _, network := range al.networks {
		if network.Contains(ip) {
			al.recordAllowed()
			return true
		}
	}

	al.recordDenied()
	return false
}

// IsAddrAllowed checks if an address (host:port or IP) is allowed
func (al *IPAllowlist) IsAddrAllowed(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ErrInvalidAddress
	}

	// Extract host from address
	host := addr
	if h, _, err := SplitHostPortLoose(addr); err == nil && h != "" {
		host = h
	}

	// Try to parse as IP first
	if ip := net.ParseIP(host); ip != nil {
		if al.IsAllowed(ip) {
			return nil
		}
		return ErrAddressNotAllowed
	}

	// Handle hostname (with DNS security checks)
	if al.config.DisableDNS {
		if al.config.Logger != nil {
			al.config.Logger.Warn("DNS resolution disabled, rejecting hostname",
				ZapString("hostname", host))
		}
		return fmt.Errorf("%w: DNS disabled, use IP addresses", ErrAddressNotAllowed)
	}

	// Resolve with caching and validation
	ips, err := al.resolveWithCache(host)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrAddressNotAllowed, err)
	}

	// Check if any resolved IP is allowed
	for _, ip := range ips {
		if al.IsAllowed(ip) {
			return nil
		}
	}

	return ErrAddressNotAllowed
}

// ValidateBeforeConnect performs connection-time validation
func (al *IPAllowlist) ValidateBeforeConnect(ctx context.Context, addr string) error {
	if !al.config.ValidateDNSOnUse {
		return al.IsAddrAllowed(addr)
	}

	// Re-resolve hostname to detect DNS rebinding
	host, _, err := SplitHostPortLoose(addr)
	if err != nil {
		return err
	}

	// If it's an IP, just validate it
	if ip := net.ParseIP(host); ip != nil {
		if al.IsAllowed(ip) {
			return nil
		}
		return ErrAddressNotAllowed
	}

	// For hostnames, check if DNS changed suspiciously
	cached := al.getCachedDNS(host)
	if cached != nil {
		// Re-resolve to check for changes
		resolver := &net.Resolver{}
		ips, err := resolver.LookupIP(ctx, "ip", host)
		if err != nil {
			return fmt.Errorf("DNS lookup failed: %w", err)
		}

		// Check if IPs changed (potential rebinding)
		if !al.ipsMatch(cached.ips, ips) {
			if al.config.Logger != nil {
				al.config.Logger.Warn("DNS rebinding detected",
					ZapString("hostname", host),
					ZapAny("cached_ips", cached.ips),
					ZapAny("new_ips", ips))
			}
			return ErrDNSRebinding
		}
	}

	return al.IsAddrAllowed(addr)
}

// GetMetrics returns allowlist metrics
func (al *IPAllowlist) GetMetrics() map[string]uint64 {
	al.metricsMu.RLock()
	defer al.metricsMu.RUnlock()

	return map[string]uint64{
		"allowed_count": al.allowedCount,
		"denied_count":  al.deniedCount,
	}
}

// Private methods

func (al *IPAllowlist) checkIPType(ip net.IP) bool {
	if isPrivateIP(ip) && !al.config.AllowPrivate {
		return false
	}

	if ip.IsLoopback() && !al.config.AllowLoopback {
		return false
	}

	if ip.IsLinkLocalUnicast() && !al.config.AllowLinkLocal {
		return false
	}

	if ip.IsMulticast() && !al.config.AllowMulticast {
		return false
	}

	return true
}

func (al *IPAllowlist) resolveWithCache(hostname string) ([]net.IP, error) {
	// Check cache first
	if entry := al.getCachedDNS(hostname); entry != nil {
		if time.Since(entry.timestamp) < al.config.DNSCacheTTL {
			return entry.ips, nil
		}
	}

	// Resolve DNS
	ips, err := net.LookupIP(hostname)
	if err != nil {
		return nil, err
	}

	// Update cache
	al.setCachedDNS(hostname, ips)

	// Evict oldest if cache too large
	al.evictOldestIfNeeded()

	return ips, nil
}

func (al *IPAllowlist) getCachedDNS(hostname string) *dnsCacheEntry {
	al.dnsCacheMu.RLock()
	defer al.dnsCacheMu.RUnlock()
	return al.dnsCache[hostname]
}

func (al *IPAllowlist) setCachedDNS(hostname string, ips []net.IP) {
	al.dnsCacheMu.Lock()
	defer al.dnsCacheMu.Unlock()

	al.dnsCache[hostname] = &dnsCacheEntry{
		ips:       ips,
		timestamp: time.Now(),
		hostname:  hostname,
	}
}

func (al *IPAllowlist) ipsMatch(a, b []net.IP) bool {
	if len(a) != len(b) {
		return false
	}

	aMap := make(map[string]bool)
	for _, ip := range a {
		aMap[ip.String()] = true
	}

	for _, ip := range b {
		if !aMap[ip.String()] {
			return false
		}
	}

	return true
}

func (al *IPAllowlist) recordAllowed() {
	al.metricsMu.Lock()
	al.allowedCount++
	al.metricsMu.Unlock()
}

func (al *IPAllowlist) recordDenied() {
	al.metricsMu.Lock()
	al.deniedCount++
	al.metricsMu.Unlock()
}

// Utility functions

// ParseCIDRs parses CIDR strings into IPNet slice
func ParseCIDRs(list []string) []net.IPNet {
	var out []net.IPNet
	for _, s := range list {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}

		// Handle CIDR notation
		if strings.Contains(s, "/") {
			if _, n, err := net.ParseCIDR(s); err == nil {
				out = append(out, *n)
			}
			continue
		}

		// Single IP -> host network
		if ip := net.ParseIP(s); ip != nil {
			mask := net.CIDRMask(32, 32)
			if ip.To4() == nil {
				mask = net.CIDRMask(128, 128)
			}
			out = append(out, net.IPNet{IP: ip, Mask: mask})
		}
	}
	return out
}

// IsIPAllowed checks if IP is in allowed networks (legacy compatibility)
func IsIPAllowed(ip net.IP, allowed []net.IPNet) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, n := range allowed {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// IsAddrAllowed checks if address is allowed (legacy compatibility)
func IsAddrAllowed(addr string, allowed []net.IPNet) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}

	host := addr
	if h, _, err := SplitHostPortLoose(addr); err == nil && h != "" {
		host = h
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}

	return IsIPAllowed(ip, allowed)
}

// isPrivateIP checks if IP is in private ranges (RFC1918, RFC4193)
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	privateRanges := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"fc00::/7", // IPv6 ULA
	}

	for _, cidr := range privateRanges {
		_, network, _ := net.ParseCIDR(cidr)
		if network != nil && network.Contains(ip) {
			return true
		}
	}

	return false
}

// IsPrivateIP checks if IP is private (public API)
func IsPrivateIP(ip net.IP) bool {
	return isPrivateIP(ip)
}

// Multiaddr utilities

// ParseMultiaddr parses a multiaddr string
func ParseMultiaddr(s string) (multiaddr.Multiaddr, error) {
	ma, err := multiaddr.NewMultiaddr(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidMultiaddr, err)
	}
	return ma, nil
}

// ValidateMultiaddr validates a multiaddr for security
func ValidateMultiaddr(ma multiaddr.Multiaddr, allowlist *IPAllowlist) error {
	// Extract IP from multiaddr
	var ip net.IP

	multiaddr.ForEach(ma, func(c multiaddr.Component) bool {
		switch c.Protocol().Code {
		case multiaddr.P_IP4:
			ip = net.ParseIP(c.Value())
			return false
		case multiaddr.P_IP6:
			ip = net.ParseIP(c.Value())
			return false
		}
		return true
	})

	if ip == nil {
		return fmt.Errorf("%w: no IP found", ErrInvalidMultiaddr)
	}

	if allowlist != nil && !allowlist.IsAllowed(ip) {
		return ErrAddressNotAllowed
	}

	return nil
}

// Address parsing

// SplitHostPortLoose splits host:port with IPv6 support
func SplitHostPortLoose(s string) (host, port string, err error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", ErrInvalidAddress
	}

	// Bracketed IPv6: [::1]:1234
	if strings.HasPrefix(s, "[") {
		i := strings.IndexByte(s, ']')
		if i < 0 {
			return "", "", fmt.Errorf("%w: malformed IPv6", ErrInvalidAddress)
		}
		host = strings.Trim(s[:i+1], "[]")
		rest := s[i+1:]
		if strings.HasPrefix(rest, ":") {
			port = rest[1:]
		}
		return host, port, nil
	}

	// Last colon is separator if present
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		// Check if it's IPv6 without brackets (has multiple colons)
		if strings.Count(s, ":") > 1 {
			// Likely IPv6 without brackets
			return s, "", nil
		}
		host = s[:i]
		port = s[i+1:]
		return host, port, nil
	}

	return s, "", nil
}

// Port utilities

// PortFromRange calculates a port from a range
func PortFromRange(start, size, index int) (int, error) {
	if start <= 0 || start > 65535 {
		return 0, fmt.Errorf("%w: invalid start port %d", ErrPortOutOfRange, start)
	}
	if size <= 0 {
		return 0, fmt.Errorf("%w: invalid size %d", ErrPortOutOfRange, size)
	}

	port := start + (index % size)
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("%w: calculated port %d", ErrPortOutOfRange, port)
	}

	return port, nil
}

// PortInRange checks if port is in range
func PortInRange(start, size, port int) bool {
	if start <= 0 || size <= 0 {
		return false
	}
	return port >= start && port < start+size
}

// IsPortValid checks if port is valid
func IsPortValid(port int) bool {
	return port >= 1 && port <= 65535
}

// IsWellKnownPort checks if port is well-known (< 1024)
func IsWellKnownPort(port int) bool {
	return port >= 1 && port < 1024
}

// Address fingerprinting

// FingerprintAddress creates a deterministic hash of an address
func FingerprintAddress(addr string) string {
	hash := sha256.Sum256([]byte(addr))
	return hex.EncodeToString(hash[:8])
}

// Connection utilities

// DialContext dials with IP allowlist validation
func DialContext(ctx context.Context, network, address string, allowlist *IPAllowlist) (net.Conn, error) {
	// Validate before connecting
	if allowlist != nil {
		if err := allowlist.ValidateBeforeConnect(ctx, address); err != nil {
			return nil, err
		}
	}

	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

// DialContextWithTimeout dials with timeout and validation
func DialContextWithTimeout(ctx context.Context, network, address string, timeout time.Duration, allowlist *IPAllowlist) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return DialContext(ctx, network, address, allowlist)
}

func (al *IPAllowlist) evictOldestIfNeeded() {
	if len(al.dnsCache) <= al.maxCacheSize {
		return
	}

	// Find and remove oldest entry
	var oldest string
	var oldestTime time.Time = time.Now()
	for hostname, entry := range al.dnsCache {
		if entry.timestamp.Before(oldestTime) {
			oldestTime = entry.timestamp
			oldest = hostname
		}
	}
	if oldest != "" {
		delete(al.dnsCache, oldest)
	}
}
