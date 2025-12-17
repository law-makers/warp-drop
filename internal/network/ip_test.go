package network

import (
	"net"
	"testing"
)

func TestIsPrivateIPv4(t *testing.T) {
	cases := []struct{
		ip string
		want bool
	}{
		{"127.0.0.1", false},
		{"192.168.1.5", true},
		{"10.0.0.12", true},
		{"172.20.3.4", true},
		{"8.8.8.8", false},
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip).To4()
		if ip == nil {
			t.Fatalf("invalid test IP: %s", c.ip)
		}
		got := isPrivateIPv4(ip)
		if got != c.want {
			t.Errorf("isPrivateIPv4(%s) = %v, want %v", c.ip, got, c.want)
		}
	}
}
