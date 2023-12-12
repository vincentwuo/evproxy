package util

import (
	"fmt"
	"net"
	"syscall"
)

var DEBUG bool

func Println(a ...any) {
	if !DEBUG {
		return
	}
	fmt.Println(a)
}

// SockaddrToTCPOrUnixAddr converts a Sockaddr to a net.TCPAddr or net.UnixAddr.
// Returns nil if conversion fails.
func SockaddrToTCPOrUnixAddr(sa syscall.Sockaddr) net.Addr {
	switch sa := sa.(type) {
	case *syscall.SockaddrInet4:
		return &net.TCPAddr{IP: sa.Addr[0:], Port: sa.Port}
	case *syscall.SockaddrInet6:
		return &net.TCPAddr{IP: sa.Addr[0:], Port: sa.Port}
	case *syscall.SockaddrUnix:
		return &net.UnixAddr{Name: sa.Name, Net: "unix"}
	}
	return nil
}

// SockaddrToUDPAddr converts a Sockaddr to a net.UDPAddr
// Returns nil if conversion fails.
func SockaddrToUDPAddr(sa syscall.Sockaddr) net.Addr {
	switch sa := sa.(type) {
	case *syscall.SockaddrInet4:
		return &net.UDPAddr{IP: sa.Addr[0:], Port: sa.Port}
	case *syscall.SockaddrInet6:
		return &net.UDPAddr{IP: sa.Addr[0:], Port: sa.Port}
	}
	return nil
}

// // ip6ZoneToString converts an IP6 Zone unix int to a net string,
// // returns "" if zone is 0.
//
//	func ip6ZoneToString(zone uint32) string {
//		if zone == 0 {
//			return ""
//		}
//		if ifi, err := net.InterfaceByIndex(int(zone)); err == nil {
//			return ifi.Name
//		}
//		return uint2decimalStr(uint(zone))
//	}
func ipToSockaddrInet4(ip net.IP, port int) (syscall.SockaddrInet4, error) {
	if len(ip) == 0 {
		ip = net.IPv4zero
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return syscall.SockaddrInet4{}, &net.AddrError{Err: "non-IPv4 address", Addr: ip.String()}
	}
	sa := syscall.SockaddrInet4{Port: port}
	copy(sa.Addr[:], ip4)
	return sa, nil
}

func IpToSockaddr(family int, ip net.IP, port int, zone string) (syscall.Sockaddr, error) {
	switch family {
	case syscall.AF_INET:
		sa, err := ipToSockaddrInet4(ip, port)
		if err != nil {
			return nil, err
		}
		return &sa, nil
	case syscall.AF_INET6:
		return nil, &net.AddrError{Err: "IPV6 not supported", Addr: ip.String()}
	}
	return nil, &net.AddrError{Err: "invalid address family", Addr: ip.String()}
}
