package ff200

import (
	"bytes"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestRun(t *testing.T) {
	proxies := []string{"127.0.0.1:9050", "127.0.0.1:9051", "127.0.0.1:9052", "127.0.0.1:9063", "127.0.0.1:9054", "127.0.0.1:9055", "127.0.0.1:9056", "127.0.0.1:9057", "127.0.0.1:9058", "127.0.0.1:9059"}
	url := "http://duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion"

	body, proxy, err := Run(proxies, url, &Options{Debug: true})
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(body) == 0 {
		t.Error("Expected non-empty response body")
	}
	t.Logf("Success via proxy: %s, received %d bytes", proxy, len(body))
}
func isValidJSON(s string) bool {
	var js json.RawMessage
	return json.Unmarshal([]byte(s), &js) == nil
}

const (
	duckduckgoOnion = "http://duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion"
)

// getTorProxies returns the list of local Tor SOCKS5 proxies
func getTorProxies() []string {
	proxies := make([]string, 6)
	for i := 0; i < 6; i++ {
		proxies[i] = fmt.Sprintf("127.0.0.1:%d", 9050+i)
	}
	return proxies
}

func TestBasicFunctionality(t *testing.T) {
	proxies := getTorProxies()

	opts := &Options{
		Timeout: 30 * time.Second,
		Debug:   true,
	}

	body, proxy, err := Run(proxies, duckduckgoOnion, opts)
	if err != nil {
		t.Fatalf("Expected success, got error: %v", err)
	}

	if len(body) == 0 {
		t.Fatal("Expected non-empty body")
	}

	if proxy == "" {
		t.Fatal("Expected proxy address to be returned")
	}

	if !bytes.Contains(body, []byte("DuckDuckGo")) {
		t.Error("Expected DuckDuckGo content in response")
	}

	t.Logf("Success via proxy: %s, received %d bytes", proxy, len(body))
}

func TestNoProxies(t *testing.T) {
	_, _, err := Run([]string{}, duckduckgoOnion, &Options{Timeout: 5 * time.Second})
	if err == nil {
		t.Fatal("Expected error with no proxies")
	}
	if err.Error() != "no proxies provided" {
		t.Errorf("Expected 'no proxies provided' error, got: %v", err)
	}
}

func TestTimeoutHandling(t *testing.T) {
	proxies := getTorProxies()

	// Very short timeout - should fail
	opts := &Options{
		Timeout: 100 * time.Millisecond,
		Debug:   true,
	}

	start := time.Now()
	_, _, err := Run(proxies, duckduckgoOnion, opts)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Expected timeout error with very short timeout")
	}

	// Should timeout relatively quickly (within 2x the timeout + some buffer)
	maxExpected := opts.Timeout * 3
	if elapsed > maxExpected {
		t.Errorf("Timeout took too long: expected ~%v, got %v", opts.Timeout, elapsed)
	}

	t.Logf("Timeout correctly handled in %v", elapsed)
}

func TestAllProxiesFail(t *testing.T) {
	// Use invalid proxies
	invalidProxies := []string{
		"127.0.0.1:19999",
		"127.0.0.1:29999",
		"127.0.0.1:39999",
	}

	opts := &Options{
		Timeout: 2 * time.Second,
		Debug:   true,
	}

	_, _, err := Run(invalidProxies, duckduckgoOnion, opts)
	if err == nil {
		t.Fatal("Expected error when all proxies fail")
	}

	t.Logf("Correctly failed with: %v", err)
}

func TestConcurrentRequests(t *testing.T) {
	proxies := getTorProxies()
	opts := &Options{
		Timeout: 30 * time.Second,
		Debug:   false,
	}

	concurrency := 20
	var wg sync.WaitGroup
	errors := make(chan error, concurrency)
	successes := make(chan string, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			body, proxy, err := Run(proxies, duckduckgoOnion, opts)
			if err != nil {
				errors <- fmt.Errorf("request %d: %v", id, err)
				return
			}
			if len(body) > 0 {
				successes <- fmt.Sprintf("request %d via %s (%d bytes)", id, proxy, len(body))
			}
		}(i)
	}

	wg.Wait()
	close(errors)
	close(successes)

	// Collect results
	var errCount int
	for err := range errors {
		t.Logf("Error: %v", err)
		errCount++
	}

	var successCount int
	for msg := range successes {
		t.Logf("Success: %s", msg)
		successCount++
	}

	if successCount == 0 {
		t.Fatal("Expected at least some concurrent requests to succeed")
	}

	t.Logf("Concurrent test: %d successes, %d failures out of %d requests",
		successCount, errCount, concurrency)
}

func TestContextCancellation(t *testing.T) {
	proxies := getTorProxies()
	opts := &Options{
		Timeout: 60 * time.Second, // Long timeout
		Debug:   true,
	}

	// Start request in goroutine
	done := make(chan struct{})
	var body []byte
	var proxy string
	var err error

	go func() {
		body, proxy, err = Run(proxies, duckduckgoOnion, opts)
		close(done)
	}()

	// Give it a moment to start
	time.Sleep(500 * time.Millisecond)

	// The context is internal, but we can test that it doesn't hang forever
	select {
	case <-done:
		if err == nil && len(body) > 0 {
			t.Logf("Request completed successfully via %s", proxy)
		} else {
			t.Logf("Request failed as expected: %v", err)
		}
	case <-time.After(35 * time.Second):
		t.Fatal("Request hung - context cancellation not working properly")
	}
}

func TestMixedProxies(t *testing.T) {
	// Mix of valid and invalid proxies
	mixedProxies := []string{
		"127.0.0.1:19999", // Invalid
		"127.0.0.1:9050",  // Valid (Tor)
		"127.0.0.1:29999", // Invalid
		"127.0.0.1:9051",  // Valid (Tor)
	}

	opts := &Options{
		Timeout: 20 * time.Second,
		Debug:   true,
	}

	body, proxy, err := Run(mixedProxies, duckduckgoOnion, opts)
	if err != nil {
		t.Fatalf("Expected success with mixed proxies, got: %v", err)
	}

	if len(body) == 0 {
		t.Fatal("Expected non-empty body")
	}

	// Should succeed via one of the valid Tor proxies
	if proxy != "127.0.0.1:9050" && proxy != "127.0.0.1:9051" {
		t.Errorf("Expected success via valid Tor proxy, got: %s", proxy)
	}

	t.Logf("Success via %s with mixed proxy list", proxy)
}

func TestSingleProxy(t *testing.T) {
	proxies := []string{"127.0.0.1:9050"}

	opts := &Options{
		Timeout: 30 * time.Second,
		Debug:   true,
	}

	body, proxy, err := Run(proxies, duckduckgoOnion, opts)
	if err != nil {
		t.Fatalf("Single proxy test failed: %v", err)
	}

	if proxy != "127.0.0.1:9050" {
		t.Errorf("Expected proxy 127.0.0.1:9050, got: %s", proxy)
	}

	if len(body) == 0 {
		t.Fatal("Expected non-empty body")
	}

	t.Logf("Single proxy success: %d bytes", len(body))
}

// Benchmark to detect memory leaks
func BenchmarkMemoryUsage(b *testing.B) {
	proxies := getTorProxies()
	opts := &Options{
		Timeout: 20 * time.Second,
		Debug:   false,
	}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, _, err := Run(proxies, duckduckgoOnion, opts)
		if err != nil {
			// Some failures are expected in benchmarks
			continue
		}
	}
}

func BenchmarkConcurrentRequests(b *testing.B) {
	proxies := getTorProxies()
	opts := &Options{
		Timeout: 20 * time.Second,
		Debug:   false,
	}

	b.ReportAllocs()
	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _, _ = Run(proxies, duckduckgoOnion, opts)
		}
	})
}

// Test with default options
func TestDefaultOptions(t *testing.T) {
	proxies := []string{"127.0.0.1:9050"}

	body, proxy, err := Run(proxies, duckduckgoOnion)
	if err != nil {
		t.Fatalf("Default options test failed: %v", err)
	}

	if len(body) == 0 {
		t.Fatal("Expected non-empty body with default options")
	}

	t.Logf("Default options success via %s: %d bytes", proxy, len(body))
}

// Stress test - run many requests to check for race conditions
func TestStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	proxies := getTorProxies()
	opts := &Options{
		Timeout: 30 * time.Second,
		Debug:   false,
	}

	iterations := 100
	var successCount, failCount int
	var mu sync.Mutex

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10) // Limit concurrency to 10

	for i := 0; i < iterations; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			_, _, err := Run(proxies, duckduckgoOnion, opts)
			mu.Lock()
			if err != nil {
				failCount++
			} else {
				successCount++
			}
			mu.Unlock()
		}(i)
	}

	wg.Wait()

	successRate := float64(successCount) / float64(iterations) * 100
	t.Logf("Stress test: %d/%d successful (%.1f%%)", successCount, iterations, successRate)

	if successCount == 0 {
		t.Fatal("All requests failed in stress test")
	}
}
func TestGoroutineLeaksWithDump(t *testing.T) {
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	t.Logf("Baseline goroutines: %d", baseline)

	proxies := getTorProxies()
	opts := &Options{
		Timeout: 20 * time.Second,
		Debug:   false,
	}

	// Single iteration
	body, proxy, err := Run(proxies, duckduckgoOnion, opts)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	t.Logf("Success via %s (%d bytes)", proxy, len(body))

	// Force cleanup
	runtime.GC()
	time.Sleep(2 * time.Second)
	runtime.GC()
	time.Sleep(2 * time.Second)

	final := runtime.NumGoroutine()
	leaked := final - baseline
	t.Logf("After 1 iteration: baseline=%d, final=%d, leaked=%d", baseline, final, leaked)

	if leaked > 2 {
		// Dump goroutine stack traces
		buf := make([]byte, 1<<20)
		stackSize := runtime.Stack(buf, true)
		t.Logf("Goroutine dump:\n%s", buf[:stackSize])
		t.Errorf("Goroutine leak detected: %d leaked", leaked)
	}
}

func TestGoroutineLeaks(t *testing.T) {
	// Force GC and get baseline
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	baseline := runtime.NumGoroutine()

	t.Logf("Baseline goroutines: %d", baseline)

	proxies := getTorProxies()
	opts := &Options{
		Timeout: 20 * time.Second,
		Debug:   false,
	}

	// Run multiple times to detect leaks
	iterations := 10
	for i := 0; i < iterations; i++ {
		body, proxy, err := Run(proxies, duckduckgoOnion, opts)
		if err != nil {
			t.Logf("Iteration %d failed (expected occasionally): %v", i, err)
			continue
		}
		if len(body) == 0 {
			t.Errorf("Iteration %d: empty body", i)
		}
		t.Logf("Iteration %d: success via %s", i, proxy)

		// CRITICAL: Allow time for goroutine cleanup between iterations
		time.Sleep(500 * time.Millisecond)
	}

	// Force GC and wait for cleanup - be more aggressive
	runtime.GC()
	time.Sleep(1 * time.Second)
	runtime.GC()
	time.Sleep(1 * time.Second)
	runtime.GC()
	time.Sleep(1 * time.Second)

	final := runtime.NumGoroutine()
	t.Logf("Final goroutines: %d", final)

	// Allow some tolerance (a couple goroutines is acceptable)
	leaked := final - baseline
	if leaked > 3 {
		t.Errorf("Goroutine leak detected: baseline=%d, final=%d, leaked=%d", baseline, final, leaked)

		// Dump stack traces
		buf := make([]byte, 1<<20)
		stackSize := runtime.Stack(buf, true)
		t.Logf("Goroutine dump:\n%s", buf[:stackSize])
	} else {
		t.Logf("No significant goroutine leak: leaked=%d", leaked)
	}
}

func TestGoroutineLeaksRealistic(t *testing.T) {
	proxies := getTorProxies()
	opts := &Options{
		Timeout: 20 * time.Second,
		Debug:   false,
	}

	// Warmup - first call may create some persistent goroutines
	_, _, _ = Run(proxies, duckduckgoOnion, opts)

	// Allow warmup cleanup
	runtime.GC()
	time.Sleep(2 * time.Second)

	// Now get baseline after warmup
	baseline := runtime.NumGoroutine()
	t.Logf("Baseline goroutines (after warmup): %d", baseline)

	// Run multiple iterations
	iterations := 10
	for i := 0; i < iterations; i++ {
		body, proxy, err := Run(proxies, duckduckgoOnion, opts)
		if err != nil {
			t.Logf("Iteration %d failed: %v", i, err)
			continue
		}
		t.Logf("Iteration %d: success via %s (%d bytes)", i, proxy, len(body))

		// Small delay between iterations
		time.Sleep(200 * time.Millisecond)
	}

	// Aggressive cleanup
	for i := 0; i < 5; i++ {
		runtime.GC()
		time.Sleep(500 * time.Millisecond)
	}

	final := runtime.NumGoroutine()
	leaked := final - baseline
	t.Logf("Final goroutines: %d (leaked: %d)", final, leaked)

	// After warmup, there should be NO new leaks
	if leaked > 2 {
		buf := make([]byte, 1<<20)
		stackSize := runtime.Stack(buf, true)
		t.Logf("Goroutine dump:\n%s", buf[:stackSize])
		t.Errorf("Goroutine leak detected: baseline=%d, final=%d, leaked=%d", baseline, final, leaked)
	} else {
		t.Logf("✓ No goroutine leaks detected")
	}
}
