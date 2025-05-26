package ff200

import (
	"testing"
)

func TestRun(t *testing.T) {
	proxies := []string{"127.0.0.1:9050", "127.0.0.1:9051", "127.0.0.1:9052", "127.0.0.1:9063", "127.0.0.1:9054", "127.0.0.1:9055", "127.0.0.1:9056", "127.0.0.1:9057", "127.0.0.1:9058", "127.0.0.1:9059"}
	url := "http://duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion"

	body, proxy, err := Run(proxies, url)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(body) == 0 {
		t.Error("Expected non-empty response body")
	}
	t.Logf("Success via proxy: %s, received %d bytes", proxy, len(body))
}
