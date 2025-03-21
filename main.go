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

	"github.com/Jipok/go-persist"
)

// Command-line flags for configuration
var (
	maxKeySize     = flag.Int("maxKeySize", 100, "maximum allowed key length in bytes")
	maxValueSize   = flag.Int("maxValueSize", 1000, "maximum allowed value size in bytes")
	maxNumKV       = flag.Int("maxNumKV", 100000, "maximum number of key-value pairs allowed")
	expireDuration = flag.Duration("expireDuration", 2*time.Hour, "duration after which a key expires")
	resetDuration  = flag.Duration("resetDuration", time.Minute, "duration between resets of the requests rate limit")
	saveDuration   = flag.Duration("saveDuration", 30*time.Minute, "duration between automatic state saves")
	maxRequests    = flag.Int("maxRequests", 11, "maximum request tokens per IP per resetDuration (POST=3 tokens, GET=1 token)")
	port           = flag.String("port", "80", "port on which the server listens")
	listen         = flag.String("l", "0.0.0.0", "interface to listen")
	disableWarning = flag.Bool("disableLocalIPWaring", false, "disable warnings about requests from localhost")
)

//go:embed index.html
var indexHtml []byte
var indexHtmlGz []byte

// Entry represents a stored key-value pair
type Entry struct {
	Value      []byte `json:"v"`           // stored value (can be binary)
	Secret     string `json:"s,omitempty"` // secret for key ownership (empty if not owned)
	LastUpdate int64  `json:"t"`           // timestamp of last update
}

var (
	kvStore = persist.New()
	kvMap   *persist.PersistMap[*Entry] // stores key -> *Entry.

	// rateLimit is a map storing available request per IP
	rateLimit = make(map[[4]byte]uint8)
	// mu protects postRateLimit
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

	if !*disableWarning && remoteIPStr == "127.0.0.1" {
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

	// Get the real client IP address, considering proxy headers
	parsedIP, stringIP := getRealIP(r)
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

	// Rate limiting
	mu.Lock()
	// If no requests registered for this IP, assume default
	availableTokens, exists := rateLimit[ipKey]
	if !exists {
		availableTokens = uint8(*maxRequests)
	}
	if r.Method == http.MethodPost {
		// Check if there are at least 3 tokens for a POST request
		if availableTokens < 3 {
			mu.Unlock()
			http.Error(w, "Rate limit", http.StatusTooManyRequests)
			return
		}
		availableTokens -= 3
	} else {
		// Check if there is at least 1 token for a GET request
		if availableTokens < 1 {
			mu.Unlock()
			http.Error(w, "Rate limit", http.StatusTooManyRequests)
			return
		}
		availableTokens -= 1
	}
	// Update the requests counter for this IP
	rateLimit[ipKey] = availableTokens
	mu.Unlock()

	// Special handling for /ip/ paths in POST requests
	if len(key) > 3 && key[:3] == "ip/" && r.Method == http.MethodPost {
		remainder := key[3:] // part after "ip/"
		// Automatically prefix POST keys with client's IP
		key = "ip/" + stringIP + "/" + remainder
	}

	handleKeyRequest(w, r, key)
}

// handleKeyRequest processes GET and POST for a specific key
func handleKeyRequest(w http.ResponseWriter, r *http.Request, key string) {
	switch r.Method {
	case http.MethodPost:
		authSecret := r.Header.Get("X-Owner-Secret")
		// Check that the secret alone does not exceed maxValueSize
		if len(authSecret) > *maxValueSize {
			http.Error(w, "Value plus secret too large", http.StatusBadRequest)
			return
		}
		// Calculate the maximum allowed length for the value after taking the secret into account
		allowedValueSize := *maxValueSize - len(authSecret)
		// Read the value from the request body with the adjusted limit
		body, err := io.ReadAll(io.LimitReader(r.Body, int64(allowedValueSize)+1))
		if err != nil {
			http.Error(w, "Error reading body", http.StatusInternalServerError)
			return
		}
		if len(body) > allowedValueSize {
			if authSecret != "" {
				http.Error(w, "Value plus secret too large", http.StatusBadRequest)
			} else {
				http.Error(w, "Value too large", http.StatusBadRequest)
			}
			return
		}

		now := time.Now()
		if entry, exists := kvMap.Get(key); exists {
			// If the key is owned (non-empty secret) then the provided secret must match
			if entry.Secret != "" && entry.Secret != authSecret {
				http.Error(w, "Forbidden: Incorrect secret", http.StatusForbidden)
				return
			}
			// If the key is not yet owned and the client provides a secret, register it
			if entry.Secret == "" && authSecret != "" {
				entry.Secret = authSecret
			}
			entry.Value = body
			entry.LastUpdate = now.Unix()
		} else {
			if kvMap.Size() >= *maxNumKV {
				http.Error(w, "Store capacity reached", http.StatusInsufficientStorage)
				return
			}
			kvMap.SetAsync(key, &Entry{
				Value:      body,
				Secret:     authSecret,
				LastUpdate: now.Unix(),
			})
		}

		// For ip keys, return client's IP address in the response instead of "OK"
		if strings.HasPrefix(key, "ip/") {
			_, ipStr := getRealIP(r)
			w.Write([]byte(ipStr))
		} else {
			w.Write([]byte("OK"))
		}

	case http.MethodGet:
		entry, exists := kvMap.Get(key)
		if !exists {
			http.Error(w, "Key not found", http.StatusNotFound)
			return
		}

		// Copy the stored value
		value := entry.Value

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
		kvMap.Range(func(key string, entry *Entry) bool {
			if now.Sub(time.Unix(entry.LastUpdate, 0)) > *expireDuration {
				kvMap.Delete(key)
				expiredCount++
			}
			return true
		})
		if expiredCount > 0 {
			log.Printf("Cleaned up %d expired keys", expiredCount)
		}
	}
}

// resetRateLimit resets the map storing requests counter per IP
func resetRateLimit() {
	for {
		time.Sleep(*resetDuration)
		mu.Lock()
		rateLimit = make(map[[4]byte]uint8)
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
	precompressIndexHtml()

	var err error
	kvMap, err = persist.Map[*Entry](kvStore, "kv")
	if err != nil {
		log.Fatal(err)
	}

	err = kvStore.Open("store.db")
	if err != nil {
		log.Fatal(err)
	}
	defer kvStore.Close()

	kvStore.SetSyncInterval(*saveDuration)
	go cleanupExpiredKeys()
	go resetRateLimit()

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

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	log.Println("Server is starting on http://" + addr)
	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
