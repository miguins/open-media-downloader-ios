package extractor

import "testing"

func TestYTDLPRejectsNonLoopbackProxy(t *testing.T) {
	if validProxyURL("http://example.com:8080") {
		t.Fatal("accepted remote proxy")
	}
	if !validProxyURL("http://127.0.0.1:8080") {
		t.Fatal("rejected loopback proxy")
	}
}
