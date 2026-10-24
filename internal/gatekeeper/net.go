package gatekeeper

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/netip"
	"time"
)

// newCA creates a fresh signing CA for each gatekeeper run. The private key only lives in memory;
// the desktop app fetches the public certificate through GET /ca and installs it in the sandbox.
func newCA() (tls.Certificate, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "AIofCage gatekeeper CA", Organization: []string{"AIofCage"}},
		NotBefore:    time.Now().Add(-time.Hour), NotAfter: time.Now().Add(30 * 24 * time.Hour),
		IsCA: true, BasicConstraintsValid: true, MaxPathLenZero: true,
		KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		return tls.Certificate{}, nil, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf},
		pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}

// Non-public ranges: loopback, private, link-local (cloud metadata), CGNAT, documentation,
// multicast, NAT64 and similar. The gatekeeper never connects to them on the agent's behalf.
var forbiddenPrefixes = func() []netip.Prefix {
	var result []netip.Prefix
	for _, value := range []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.88.99.0/24", "192.168.0.0/16",
		"198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::/96", "64:ff9b::/96", "64:ff9b:1::/48", "100::/64", "2001::/23", "2001:db8::/32",
		"2002::/16", "fc00::/7", "fe80::/10", "ff00::/8",
	} {
		result = append(result, netip.MustParsePrefix(value))
	}
	return result
}()

func publicIP(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.Zone() != "" {
		return false
	}
	for _, prefix := range forbiddenPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

var errDestinationDenied = errors.New("destination_denied")

// publicDial resolves on every connection, rejects the name if any answer is non-public, and
// dials the checked address directly so a second resolution cannot redirect the connection.
func publicDial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port != "443" {
		return nil, errDestinationDenied
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return nil, errDestinationDenied
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil || len(ips) == 0 {
		return nil, errors.New("dns_failed")
	}
	for _, ip := range ips {
		if !publicIP(ip) {
			return nil, errDestinationDenied
		}
	}
	dialer := net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, ip := range ips {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, errors.Join(errors.New("upstream_unavailable"), lastErr)
}
