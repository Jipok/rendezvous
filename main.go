package main

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Command-line flags for configuration
var (
	maxKeySize     = flag.Int("maxKeySize", 100, "maximum allowed key length in bytes")
	maxValueSize   = flag.Int("maxValueSize", 1000, "maximum allowed value size in bytes")
	maxNumKV       = flag.Int("maxNumKV", 100000, "maximum number of key-value pairs allowed")
	expireDuration = flag.Duration("expireDuration", 2*time.Hour, "duration after which a key expires")
	resetDuration  = flag.Duration("resetDuration", time.Minute, "duration between resets of the POST rate limit")
	saveDuration   = flag.Duration("saveDuration", 30*time.Minute, "duration between automatic state saves")
	port           = flag.String("port", "80", "port on which the server listens")
	listen         = flag.String("l", "0.0.0.0", "interface to listen")
	disableWaring  = flag.Bool("disableLocalIPWaring", false, "disable warnings about requests from localhost")
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

// getRealIP extracts the real client IP address and returns both the parsed IP and its string representation.
// It only trusts proxy headers if the request originates from a private IP.
func getRealIP(r *http.Request) (net.IP, string) {
	// Parse RemoteAddr to separate IP and port
	remoteIPStr, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteIPStr = r.RemoteAddr
	}
	remoteIP := net.ParseIP(remoteIPStr)

	// Only trust proxy headers if the request came from a trusted (private) source
	if remoteIP.IsPrivate() || remoteIP.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			// Split by comma and take the first valid IP candidate
			ips := strings.Split(xff, ",")
			for _, ipCandidate := range ips {
				ipCandidate = strings.TrimSpace(ipCandidate)
				if parsedIP := net.ParseIP(ipCandidate); parsedIP != nil {
					return parsedIP, ipCandidate
				}
			}
		}
	}

	if !*disableWaring && remoteIPStr == "127.0.0.1" {
		// Log details for diagnosing potentially misconfigured proxy requests
		referrer := r.Header.Get("Referer")
		ua := r.Header.Get("User-Agent")
		forwarded := r.Header.Get("X-Forwarded-For")
		realIP := r.Header.Get("X-Real-IP")
		fmt.Printf("WARNING: Request from localhost IP (%s). This may indicate incorrectly configured proxy.\n", remoteIPStr)
		fmt.Printf("Request: %s %s\n", r.Method, r.URL.Path)
		fmt.Printf("Referer: %s\nUser-Agent: %s\n", referrer, ua)
		fmt.Printf("Proxy headers:\n  X-Forwarded-For: %s\n  X-Real-IP(not supported): %s\n", forwarded, realIP)
	}

	return remoteIP, remoteIPStr
}

func mainHandler(w http.ResponseWriter, r *http.Request) {
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

	// Safely extract key from URL path
	key := strings.TrimPrefix(r.URL.Path, "/")
	if key == "" {
		http.Error(w, "Key is required", http.StatusBadRequest)
		return
	}

	// Check key length limit
	if len(key) > *maxKeySize {
		http.Error(w, "Key too long", http.StatusBadRequest)
		return
	}

	// Special handling for /ip/ paths
	if len(key) > 3 && key[:3] == "ip/" {
		ipKey := key[3:] // Extract the part after "/ip/"

		// For POST requests, check if the key starts with client's IP
		if r.Method == http.MethodPost {
			_, ip := getRealIP(r)
			if !strings.HasPrefix(ipKey, ip+"/") {
				http.Error(w, "Forbidden: key must start with your IP address and slash /", http.StatusForbidden)
				return
			}
		}
	}

	handleKeyRequest(w, r, key)
}

// handleKeyRequest processes GET and POST for a specific key
func handleKeyRequest(w http.ResponseWriter, r *http.Request, key string) {
	switch r.Method {
	case http.MethodPost:
		// Get the real client IP address, considering proxy headers
		parsedIP, _ := getRealIP(r)
		if parsedIP == nil {
			return // Invalid IP format
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
		Handler:                      http.HandlerFunc(mainHandler),
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
