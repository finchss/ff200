package ff200

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
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

type Options struct {
	Username string
	Password string
}

// Run attempts to fetch the URL using all provided SOCKS5 proxies concurrently.
// It returns the response body and the address of the proxy that succeeded first with HTTP 200 OK.
func Run(proxies []string, url string, opts ...*Options) ([]byte, string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var opt *Options
	if len(opts) > 0 {
		opt = opts[0]
	}

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

			netDialer, ok := dialer.(proxy.ContextDialer)
			if !ok {
				results <- fetchResult{"", nil, fmt.Errorf("dialer does not support context: %s", addr)}
				return
			}

			httpTransport := &http.Transport{
				DialContext:         netDialer.DialContext,
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

			req.Header.Set("Accept-Encoding", "gzip, deflate")

			if opt != nil && opt.Username != "" && opt.Password != "" {
				req.SetBasicAuth(opt.Username, opt.Password)
			}

			resp, err := client.Do(req)
			if err != nil {
				results <- fetchResult{"", nil, err}
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == 200 {
				var reader io.Reader = resp.Body
				if resp.Header.Get("Content-Encoding") == "gzip" {
					gzipReader, err := gzip.NewReader(resp.Body)
					if err != nil {
						results <- fetchResult{"", nil, fmt.Errorf("failed to create gzip reader for %s: %v", addr, err)}
						return
					}
					defer gzipReader.Close()
					reader = gzipReader
				}

				body, err := io.ReadAll(reader)
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
			cancel()
			return res.body, res.proxy, nil
		}
	}

	return nil, "", fmt.Errorf("all proxy requests failed or returned non-200")
}
