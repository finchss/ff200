package ff200

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
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
	Debug    bool
	Timeout  time.Duration
}

// Run attempts to fetch the URL using all provided SOCKS5 proxies concurrently.
// It returns the response body and the address of the proxy that succeeded first with HTTP 200 OK.
func Run(proxies []string, url string, opts ...*Options) ([]byte, string, error) {
	l := log.New(os.Stderr, "", log.LstdFlags|log.Lshortfile)

	var opt *Options
	if len(opts) > 0 {
		opt = opts[0]
	} else {
		l.Printf("No options provided, using default options\n")
		opt = &Options{
			Timeout: 30 * time.Second,
		}
	}

	if len(proxies) == 0 {
		return nil, "", fmt.Errorf("no proxies provided")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	results := make(chan fetchResult, len(proxies))
	var wg sync.WaitGroup

	for _, proxyAddr := range proxies {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			if opt.Debug {
				l.Printf("Attempting to fetch %s via %s\n", url, addr)
			}
			dialer, err := proxy.SOCKS5("tcp", addr, nil, proxy.Direct)
			if err != nil {
				if opt.Debug {
					l.Printf("Failed to create dialer for %s: %v\n", addr, err)
				}
				results <- fetchResult{"", nil, fmt.Errorf("failed to create dialer for %s: %v", addr, err)}
				return
			}

			netDialer, ok := dialer.(proxy.ContextDialer)
			if !ok {
				if opt.Debug {
					l.Printf("dialer does not support context: %s\n", addr)
				}
				results <- fetchResult{"", nil, fmt.Errorf("dialer does not support context: %s", addr)}
				return
			}

			httpTransport := &http.Transport{
				DialContext:         netDialer.DialContext,
				TLSHandshakeTimeout: opt.Timeout,
				DisableKeepAlives:   true,
			}

			client := &http.Client{
				Transport: httpTransport,
				Timeout:   opt.Timeout,
			}

			req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				if opt.Debug {
					l.Printf("Failed to create request for %s: %v\n", addr, err)
				}
				results <- fetchResult{"", nil, err}
				return
			}

			req.Header.Set("Accept-Encoding", "gzip, deflate")

			if opt != nil && opt.Username != "" && opt.Password != "" {
				req.SetBasicAuth(opt.Username, opt.Password)
				if opt.Debug {
					l.Printf("Using basic auth for %s\n", addr)
				}
			}

			resp, err := client.Do(req)
			if err != nil {
				if opt.Debug {
					l.Printf("Failed to fetch %s via %s: %v\n", url, addr, err)
				}
				results <- fetchResult{"", nil, err}
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode == 200 {
				var reader io.Reader = resp.Body
				if resp.Header.Get("Content-Encoding") == "gzip" {
					if opt.Debug {
						l.Printf("Got header from %s: Content-Encoding: gzip\n", proxyAddr)
					}
					gzipReader, err := gzip.NewReader(resp.Body)
					if err != nil {
						if opt.Debug {
							l.Printf("Failed to create gzip reader from %s: %v\n", proxyAddr, err)
						}
						results <- fetchResult{"", nil, fmt.Errorf("failed to create gzip reader for %s: %v", addr, err)}
						return
					}
					defer gzipReader.Close()
					reader = gzipReader
				}

				body, err := io.ReadAll(reader)
				if err != nil {
					if opt.Debug {
						l.Printf("Failed to read body from %s: %v\n", proxyAddr, err)
					}
					results <- fetchResult{"", nil, fmt.Errorf("failed to read body from %s: %v", addr, err)}
					return
				}

				results <- fetchResult{addr, body, nil}
				return
			}
			if opt.Debug {
				l.Printf("Got non-200 status from %s: %d\n", proxyAddr, resp.StatusCode)
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
			if opt.Debug {
				l.Printf("Success via proxy: %s, received %d bytes\n", res.proxy, len(res.body))
			}
			cancel()
			return res.body, res.proxy, nil
		}
	}

	return nil, "", fmt.Errorf("all proxy requests failed or returned non-200")
}
