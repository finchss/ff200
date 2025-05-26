package ff200

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

type fetchResult struct {
	proxy string
	body  []byte
	err   error
}

// Run attempts to fetch the URL using all provided SOCKS5 proxies concurrently.
// It returns the response body and the address of the proxy that succeeded first with HTTP 200 OK.
func Run(proxies []string, url string) ([]byte, string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	results := make(chan fetchResult, len(proxies))
	var wg sync.WaitGroup

	for _, proxyAddr := range proxies {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()

			dialer, err := proxy.SOCKS5("tcp", addr, nil, proxy.Direct)
			if err != nil {
				results <- fetchResult{"", nil, fmt.Errorf("failed to create dialer for %s: %v", addr, err)}
				return
			}

			netDialer := dialer.(proxy.ContextDialer)
			httpTransport := &http.Transport{
				DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
					// Do not resolve locally, let SOCKS5 proxy resolve the hostname
					return netDialer.DialContext(ctx, network, address)
				},
				TLSHandshakeTimeout: 10 * time.Second,
			}

			client := &http.Client{
				Transport: httpTransport,
				Timeout:   20 * time.Second,
			}

			req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				results <- fetchResult{"", nil, err}
				return
			}

			resp, err := client.Do(req)
			if err != nil {
				results <- fetchResult{"", nil, err}
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == 200 {
				body, err := io.ReadAll(resp.Body)
				if err != nil {
					results <- fetchResult{"", nil, fmt.Errorf("failed to read body from %s: %v", addr, err)}
					return
				}
				results <- fetchResult{addr, body, nil}
				return
			}

			results <- fetchResult{"", nil, fmt.Errorf("non-200 status from %s: %d", addr, resp.StatusCode)}
		}(proxyAddr)
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	for res := range results {
		if res.err == nil {
			cancel() // cancel other requests
			return res.body, res.proxy, nil
		}
	}

	return nil, "", fmt.Errorf("all proxy requests failed or returned non-200")
}
