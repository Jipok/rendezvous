package main

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// Command-line flags for configuration
var (
	maxKeySize     = flag.Int("maxKeySize", 100, "maximum allowed key length in bytes")
	maxValueSize   = flag.Int("maxValueSize", 1000, "maximum allowed value size in bytes")
	maxNumKV       = flag.Int("maxNumKV", 500000, "maximum number of key-value pairs allowed")
	expireDuration = flag.Duration("expireDuration", 2*time.Hour, "duration after which a key expires")
	resetDuration  = flag.Duration("resetDuration", time.Minute, "duration between resets of the POST rate limit")
	saveDuration   = flag.Duration("saveDuration", 30*time.Minute, "duration between automatic state saves")
	port           = flag.String("port", "8080", "port on which the server listens")
	listen         = flag.String("l", "0.0.0.0", "interface to listen")
)

//go:embed index.html
var indexHtml []byte
var indexHtmlGz []byte

// Entry represents a stored key-value pair
type Entry struct {
	Value      []byte    // stored value (can be binary)
	LastUpdate time.Time // timestamp of last update
}

var (
	// kvStore stores key -> *Entry.
	kvStore = make(map[string]*Entry)

	// postRateLimit is a set storing IPs which already did a POST in the current minute
	// Using struct{} as the value saves memory
	postRateLimit = make(map[[4]byte]struct{})

	// mu protects the global maps: kvStore and postRateLimit
	mu sync.RWMutex
)

// keyHandler handles GET and POST requests to /<key>
// GET returns the stored value (if exists and not expired)
// POST reads the request body as the new value
func keyHandler(w http.ResponseWriter, r *http.Request) {
	// Serve embedded index.html for the root path
	if r.URL.Path == "/" {
		if r.Method != http.MethodGet {
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "text/html")
		w.Write(indexHtmlGz)
		return
	}

	// Extract key from URL path (trim leading '/')
	key := r.URL.Path[1:]
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}
	// Check key length limit
	if len(key) > *maxKeySize {
		http.Error(w, "Key too long", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodPost:
		// Get client's IP address
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			return
		}
		parsedIP := net.ParseIP(ip)
		if parsedIP == nil {
			return
		}
		ip4 := parsedIP.To4()
		if ip4 == nil {
			http.Error(w, "Only IPv4 is supported", http.StatusBadRequest)
			return
		}
		var ipKey [4]byte
		copy(ipKey[:], ip4)

		// Rate limiting: allow only one POST per minute per IP
		mu.Lock()
		if _, exists := postRateLimit[ipKey]; exists {
			mu.Unlock()
			http.Error(w, "Rate limit", http.StatusTooManyRequests)
			return
		}
		// Mark this IP as posted
		postRateLimit[ipKey] = struct{}{}
		mu.Unlock()

		// Read value from request body with limit
		body, err := io.ReadAll(io.LimitReader(r.Body, int64(*maxValueSize)+1))
		if err != nil {
			http.Error(w, "Error reading body", http.StatusInternalServerError)
			return
		}
		if len(body) > *maxValueSize {
			http.Error(w, "Value too large", http.StatusBadRequest)
			return
		}

		now := time.Now()
		mu.Lock()
		// If the key exists, update it; otherwise create a new entry
		if entry, exists := kvStore[key]; exists {
			entry.Value = body
			entry.LastUpdate = now
		} else {
			if len(kvStore) >= *maxNumKV {
				mu.Unlock()
				http.Error(w, "Store capacity reached", http.StatusInsufficientStorage)
				return
			}
			kvStore[key] = &Entry{
				Value:      body,
				LastUpdate: now,
			}
		}
		mu.Unlock()

		w.Write([]byte("OK"))

	case http.MethodGet:
		mu.RLock()
		entry, exists := kvStore[key]
		if !exists {
			mu.RUnlock()
			http.Error(w, "Key not found", http.StatusNotFound)
			return
		}

		// Copy the stored value
		value := entry.Value
		mu.RUnlock()

		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write(value)
	}
}

// cleanupExpiredKeys periodically removes expired key-value pairs
func cleanupExpiredKeys() {
	for {
		time.Sleep(1 * time.Minute)
		now := time.Now()
		expiredCount := 0
		mu.Lock()
		for key, entry := range kvStore {
			if now.Sub(entry.LastUpdate) > *expireDuration {
				delete(kvStore, key)
				expiredCount++
			}
		}
		mu.Unlock()
		if expiredCount > 0 {
			log.Printf("Cleaned up %d expired keys", expiredCount)
		}
	}
}

// resetPostRateLimit resets the set of IPs that have made a POST every resetDuration
func resetPostRateLimit() {
	for {
		time.Sleep(*resetDuration)
		mu.Lock()
		postRateLimit = make(map[[4]byte]struct{})
		mu.Unlock()
	}
}

// gzip indexHtml
func precompressIndexHtml() {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(indexHtml); err != nil {
		log.Fatalf("Error compressing index.html: %v", err)
	}
	if err := gz.Close(); err != nil {
		log.Fatalf("Error closing gzip writer: %v", err)
	}
	indexHtmlGz = buf.Bytes()
}

func main() {
	flag.Parse()
	loadKVStore()
	precompressIndexHtml()

	go cleanupExpiredKeys()
	go resetPostRateLimit()
	go periodicSave()

	addr := *listen + ":" + *port
	server := &http.Server{
		Addr:                         addr,
		Handler:                      http.HandlerFunc(keyHandler),
		ReadTimeout:                  10 * time.Second,
		WriteTimeout:                 10 * time.Second,
		MaxHeaderBytes:               1 << 13, // 8 kb
		DisableGeneralOptionsHandler: true,
	}

	server.SetKeepAlivesEnabled(false)

	// Graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigs
		log.Printf("Received signal %v, shutting down...", sig)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		saveKVStore()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
		os.Exit(0)
	}()

	log.Println("Server is starting on http://" + addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
