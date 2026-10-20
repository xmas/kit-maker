package main

import "testing"

func TestLocalOnly(t *testing.T) {
	for url, ok := range map[string]bool{
		"http://localhost:8080":     true,
		"http://127.0.0.1:8080":     true,
		"http://[::1]:8080":         true,
		"http://kitmaker.localhost": true,
		"https://kitmaker.acme.com": false,
		"http://10.0.0.5:8080":      false,
		"http://localhost.evil.com": false,
	} {
		if err := localOnly(url); (err == nil) != ok {
			t.Errorf("localOnly(%q) = %v, want ok=%v", url, err, ok)
		}
	}
}
