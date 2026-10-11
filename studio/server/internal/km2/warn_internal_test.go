package km2

import "testing"

func TestPlainHTTPWarning(t *testing.T) {
	for base, warns := range map[string]bool{
		"https://km2.example": false, "http://localhost:8000": false, "http://127.0.0.1:8000": false, "http://[::1]:8000": false,
		"http://km2.internal.example": true, "http://10.0.0.5": true, "::bad": false,
	} {
		if got := len(plainHTTPWarning(base)) > 0; got != warns {
			t.Errorf("%s: warning = %v", base, got)
		}
	}
}
