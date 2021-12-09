package net

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

// IsRoutableIP checks that the passed string can be parsed in to a valid IPv4
// address, and that it is not a loopback or unspecified address that would not be
// reachable outside of that host device.
func IsRoutableIPv4(s string) bool {
	if ip := net.ParseIP(s); ip.To4() != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
		return true
	}
	return false
}

// DetectHostIPv4 attempts to determine the host IPv4 address by finding the
// first non-loopback device with an assigned IPv4 address.
func DetectHostIPv4() (string, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "", errors.WithStack(err)
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() == nil {
				continue
			}
			return ipnet.IP.String(), nil
		}
	}
	return "", errors.New("cannot detect host IPv4 address")
}

func SplitHostPort(addr string) (string, int, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0, err
	}
	p, _ := strconv.Atoi(port)
	return host, p, nil
}

type Address struct {
	Host string
	Port int
}

func (a *Address) String() string {
	return fmt.Sprintf("%s:%d", a.Host, a.Port)
}

func (a *Address) IsUnspecified() bool {
	return net.ParseIP(a.Host).IsUnspecified()
}

func ParseAddr(addr string) (*Address, error) {
	host, port, err := SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	return &Address{host, port}, nil
}

func FixUnspecifiedHostAddr(addr string) (string, error) {
	addr = strings.TrimPrefix(addr, "http://")
	addr = strings.TrimPrefix(addr, "https://")
	host, port, err := SplitHostPort(addr)
	if err != nil {
		return addr, err
	}
	if !net.ParseIP(host).IsUnspecified() {
		return addr, nil
	}
	host, err = DetectHostIPv4()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", host, port), nil
}
