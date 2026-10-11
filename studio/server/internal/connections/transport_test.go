package connections

import "testing"

func TestAllowedAddress(t *testing.T) {
	for host, want := range map[string]bool{
		"10.0.0.5": true, "127.0.0.1": true, "8.8.8.8": true, "fd12::1": true,
		"169.254.169.254": false, "fe80::1": false, "fd00:ec2::254": false, "100.100.100.200": false,
		"168.63.129.16": false, "64:ff9b::a9fe:a9fe": false, "::ffff:169.254.169.254": false, "0.0.0.0": false,
		"224.0.0.1": false, "not-an-ip": false,
	} {
		if got := allowedAddress(host); got != want {
			t.Errorf("allowedAddress(%s) = %v", host, got)
		}
	}
	if Transport() == nil {
		t.Error("no transport")
	}
}
