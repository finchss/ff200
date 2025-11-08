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

type Result struct {
	Body  []byte
	Proxy string
}

type Options struct {
	Username string
	Password string
	Debug    bool
	Timeout  time.Duration
}

func Run(proxies []string, url string, opts ...*Options) ([]byte, string, error) {
	if len(proxies) == 0 {
		return nil, "", fmt.Errorf("no proxies provided")
	}

	opt := getOptions(opts)
	logger := log.New(os.Stderr, "", log.LstdFlags|log.Lshortfile)

	ctx, cancel := context.WithTimeout(context.Background(), opt.Timeout)
	defer cancel()

	resultCh := make(chan Result, 1)
	var wg sync.WaitGroup

	// Collect all transports so we can close them
	transports := make([]*http.Transport, 0, len(proxies))
	var transportMu sync.Mutex

	for _, proxyAddr := range proxies {
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			transport := fetchViaProxy(ctx, addr, url, opt, logger, resultCh)
			if transport != nil {
				transportMu.Lock()
				transports = append(transports, transport)
				transportMu.Unlock()
			}
		}(proxyAddr)
	}

	// Wait for first success or timeout
	var result Result
	var hasResult bool

	select {
	case result = <-resultCh:
		hasResult = true
		cancel() // Stop other goroutines
	case <-ctx.Done():
		// Timeout
	}

	// Wait for all goroutines to finish
	wg.Wait()

	// Explicitly close all transports to clean up goroutines
	transportMu.Lock()
	for _, t := range transports {
		t.CloseIdleConnections()
	}
	transportMu.Unlock()

	if hasResult {
		return result.Body, result.Proxy, nil
	}

	return nil, "", fmt.Errorf("all proxies failed or timed out")
}

func fetchViaProxy(ctx context.Context, proxyAddr, url string, opt *Options, logger *log.Logger, resultCh chan<- Result) *http.Transport {
	if ctx.Err() != nil {
		return nil
	}

	if opt.Debug {
		logger.Printf("Attempting %s via %s", url, proxyAddr)
	}

	dialer, err := proxy.SOCKS5("tcp", proxyAddr, nil, proxy.Direct)
	if err != nil {
		if opt.Debug {
			logger.Printf("Failed to create dialer for %s: %v", proxyAddr, err)
		}
		return nil
	}

	contextDialer, ok := dialer.(proxy.ContextDialer)
	if !ok {
		if opt.Debug {
			logger.Printf("Dialer doesn't support context: %s", proxyAddr)
		}
		return nil
	}

	transport := &http.Transport{
		DialContext:           contextDialer.DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
		DisableKeepAlives:     true,
		MaxIdleConns:          -1, // Disable connection pooling entirely
		MaxIdleConnsPerHost:   -1,
		MaxConnsPerHost:       0,
		IdleConnTimeout:       1 * time.Millisecond,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   opt.Timeout,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		if opt.Debug {
			logger.Printf("Failed to create request for %s: %v", proxyAddr, err)
		}
		return transport
	}

	req.Header.Set("Accept-Encoding", "gzip, deflate")
	if opt.Username != "" && opt.Password != "" {
		req.SetBasicAuth(opt.Username, opt.Password)
	}

	resp, err := client.Do(req)
	if err != nil {
		if opt.Debug {
			logger.Printf("Request failed via %s: %v", proxyAddr, err)
		}
		return transport
	}
	defer resp.Body.Close()

	if ctx.Err() != nil {
		if opt.Debug {
			logger.Printf("Context cancelled while processing %s", proxyAddr)
		}
		return transport
	}

	if resp.StatusCode != 200 {
		if opt.Debug {
			logger.Printf("Non-200 status from %s: %d", proxyAddr, resp.StatusCode)
		}
		return transport
	}

	body, err := readBody(resp, opt.Debug, logger, proxyAddr)
	if err != nil {
		if opt.Debug {
			logger.Printf("Failed to read body from %s: %v", proxyAddr, err)
		}
		return transport
	}

	select {
	case resultCh <- Result{Body: body, Proxy: proxyAddr}:
		if opt.Debug {
			logger.Printf("Success via %s (%d bytes)", proxyAddr, len(body))
		}
	default:
		if opt.Debug {
			logger.Printf("Lost race: %s", proxyAddr)
		}
	}

	return transport
}

func readBody(resp *http.Response, debug bool, logger *log.Logger, proxyAddr string) ([]byte, error) {
	var reader io.Reader = resp.Body

	if resp.Header.Get("Content-Encoding") == "gzip" {
		if debug {
			logger.Printf("Decompressing gzip from %s", proxyAddr)
		}
		gzipReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer gzipReader.Close()
		reader = gzipReader
	}

	return io.ReadAll(reader)
}
