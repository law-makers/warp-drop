package server

import (
	"crypto/tls"
	_ "embed"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/zulfikawr/warp/internal/discovery"
	"github.com/zulfikawr/warp/internal/network"
	"github.com/zulfikawr/warp/internal/protocol"
)

//go:embed static/upload.html
var uploadPageHTML string

// Buffer pool for zero-allocation streaming
// 32KB is optimal for network-to-disk transfers on most systems
var bufferPool = sync.Pool{
	New: func() interface{} {
		b := make([]byte, 32*1024)
		return &b
	},
}

type Server struct {
	InterfaceName string
	Token         string
	SrcPath       string
	// Host mode (reverse drop)
	HostMode      bool
	UploadDir     string
	TextContent   string // If set, serves text instead of file
	ip            net.IP
	Port          int
	httpServer    *http.Server
	advertiser    *discovery.Advertiser
}

// tcpKeepAliveListener sets TCP keepalive and optimizes socket for high throughput
type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (net.Conn, error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return nil, err
	}
	// Enable TCP keepalive to detect dead connections
	tc.SetKeepAlive(true)
	tc.SetKeepAlivePeriod(3 * time.Minute)
	
	// Critical: Disable Nagle's algorithm for immediate packet transmission
	// This eliminates 40-200ms delays waiting for packet coalescing
	tc.SetNoDelay(true)
	
	// Let OS auto-tune TCP window size for optimal throughput
	// Manual buffer sizing can prevent dynamic scaling
	
	return tc, nil
}

// Start initializes and starts the HTTP server. It returns the accessible URL.
func (s *Server) Start() (string, error) {
	ip, err := network.DiscoverLANIP(s.InterfaceName)
	if err != nil {
		return "", err
	}
	s.ip = ip

	mux := http.NewServeMux()
	if s.HostMode {
		mux.HandleFunc(protocol.UploadPathPrefix, s.handleUpload)
	} else {
		mux.HandleFunc(protocol.PathPrefix, s.handleDownload)
	}

	s.httpServer = &http.Server{
		ReadTimeout:       protocol.ReadTimeout,
		ReadHeaderTimeout: 30 * time.Second,
		WriteTimeout:      protocol.WriteTimeout,
		IdleTimeout:       protocol.IdleTimeout,
		MaxHeaderBytes:    1 << 20, // 1MB
		Handler:           mux,
		// Disable HTTP/2 for lower overhead on uploads
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	// Create standard TCP listener
	ln, err := net.Listen("tcp", fmt.Sprintf("%s:0", ip.String()))
	if err != nil {
		return "", err
	}
	
	// Wrap with TCP optimizations
	tcpListener, ok := ln.(*net.TCPListener)
	if !ok {
		_ = ln.Close()
		return "", fmt.Errorf("expected TCP listener")
	}
	optimizedListener := tcpKeepAliveListener{tcpListener}
	
	addr := optimizedListener.Addr().String() // ip:port
	parts := strings.Split(addr, ":")
	if len(parts) < 2 {
		_ = optimizedListener.Close()
		return "", fmt.Errorf("unexpected listener addr: %s", addr)
	}
	portStr := parts[len(parts)-1]
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	s.Port = port

	go func() {
		_ = s.httpServer.Serve(optimizedListener)
	}()

	// Advertise via mDNS for discovery (best-effort)
	mode := "send"
	path := protocol.PathPrefix + s.Token
	if s.HostMode {
		mode = "host"
		path = protocol.UploadPathPrefix + s.Token
	}
	instance := fmt.Sprintf("warp-%s", s.Token[:6])
	adv, err := discovery.Advertise(instance, mode, s.Token, path, s.ip, s.Port)
	if err != nil {
		log.Printf("mDNS advertise failed: %v", err)
	} else {
		s.advertiser = adv
	}

	if s.HostMode {
		return fmt.Sprintf("http://%s:%d%s%s", ip.String(), s.Port, protocol.UploadPathPrefix, s.Token), nil
	}
	return fmt.Sprintf("http://%s:%d%s%s", ip.String(), s.Port, protocol.PathPrefix, s.Token), nil
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	// Expect /d/{token}
	p := strings.TrimPrefix(r.URL.Path, protocol.PathPrefix)
	if p != s.Token {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// If TextContent is set, serve text securely
	if s.TextContent != "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(s.TextContent)))
		// Prevent caching of sensitive text content
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		w.Header().Set("Pragma", "no-cache")
		w.Header().Set("Expires", "0")
		w.Write([]byte(s.TextContent))
		return
	}

	fi, err := os.Stat(s.SrcPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if fi.IsDir() {
		w.Header().Set("Content-Type", "application/zip")
		name := filepath.Base(s.SrcPath) + ".zip"
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", name))
		if err := ZipDirectory(w, s.SrcPath); err != nil {
			http.Error(w, "zip error", http.StatusInternalServerError)
		}
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filepath.Base(s.SrcPath)))
	
	// Support resumable downloads via Range headers
	f, err := os.Open(s.SrcPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" && strings.HasPrefix(rangeHeader, "bytes=") {
		// Parse Range: bytes=start-end
		rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
		var start int64
		if _, err := fmt.Sscanf(rangeSpec, "%d-", &start); err == nil && start > 0 {
			if _, err := f.Seek(start, 0); err == nil {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, fi.Size()-1, fi.Size()))
				w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.Size()-start))
				w.WriteHeader(http.StatusPartialContent)
				io.Copy(w, f)
				log.Printf("Resumed download from byte %d for %s", start, filepath.Base(s.SrcPath))
				return
			}
		}
	}
	
	// Normal full file download
	http.ServeFile(w, r, s.SrcPath)
}

// handleUpload serves a simple HTML form on GET and accepts multipart file uploads on POST.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Expect /u/{token}
	p := strings.TrimPrefix(r.URL.Path, protocol.UploadPathPrefix)
	if p != s.Token {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, uploadPageHTML)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// FAST PATH: Raw binary stream (zero parsing overhead)
	if filename := r.Header.Get("X-File-Name"); filename != "" {
		s.handleRawUpload(w, r, filename)
		return
	}

	// LEGACY PATH: Multipart form data (for backward compatibility)
	requestStart := time.Now()

	// Basic upload security and limits: limit request size if Content-Length present
	// and prevent caching of responses.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// Ensure upload dir exists
	dest := s.UploadDir
	if dest == "" {
		dest = "."
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// Set max upload size to 10GB for large file support
	r.Body = http.MaxBytesReader(w, r.Body, 10<<30) // 10GB limit

	// Use streaming multipart reader for true zero-copy I/O
	// This reads directly from network to disk without buffering entire files in RAM
	reader, err := r.MultipartReader()
	if err != nil {
		log.Printf("Failed to create multipart reader: %v", err)
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	type savedInfo struct{ Name string; Size int64 }
	var saved []savedInfo

	// Stream each file part directly to disk
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break // No more parts
		}
		if err != nil {
			log.Printf("Failed to read next part: %v", err)
			http.Error(w, "upload error", http.StatusInternalServerError)
			return
		}

		// Skip non-file fields
		if part.FileName() == "" {
			part.Close()
			continue
		}

		// Sanitize filename to prevent directory traversal
		name := filepath.Base(part.FileName())
		if name == "." || name == ".." {
			part.Close()
			continue
		}

		outPath := filepath.Join(dest, name)
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			log.Printf("Failed to create file %s: %v", name, err)
			part.Close()
			http.Error(w, "write error", http.StatusInternalServerError)
			return
		}

		// Use pooled buffer to reduce GC pressure
		bufPtr := bufferPool.Get().(*[]byte)
		buf := *bufPtr
		n, err := io.CopyBuffer(out, part, buf)
		bufferPool.Put(bufPtr)
		cerr := out.Close()
		part.Close()

		if err != nil || cerr != nil {
			log.Printf("Failed to write file %s: write_err=%v, close_err=%v", name, err, cerr)
			http.Error(w, "write error", http.StatusInternalServerError)
			return
		}

		duration := time.Since(requestStart).Seconds()
		mbps := 0.0
		if duration > 0 {
			mbps = (float64(n) * 8) / (duration * 1_000_000)
		}
		log.Printf("%s, %s received in %.2fs (%.1f Mbps)", name, formatBytes(n), duration, mbps)
		saved = append(saved, savedInfo{Name: name, Size: n})
		requestStart = time.Now() // Reset for next file
	}

	if len(saved) == 0 {
		http.Error(w, "no file provided", http.StatusBadRequest)
		return
	}

	// Simple success response (client already manages state)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// findUniqueFilename prevents file overwrites by appending (1), (2), etc.
func findUniqueFilename(dir, name string) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	
	// First try: exact match
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}

	// Collision found: try "file (1).ext", "file (2).ext", etc.
	for i := 1; i < 1000; i++ {
		newName := fmt.Sprintf("%s (%d)%s", base, i, ext)
		path = filepath.Join(dir, newName)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
	}
	
	// Fallback: Use timestamp if 1000 collisions (unlikely)
	return filepath.Join(dir, fmt.Sprintf("%s_%d%s", base, time.Now().UnixNano(), ext))
}

// handleRawUpload processes raw binary stream uploads (A+ tier performance)
// This eliminates multipart parsing overhead for maximum speed
func (s *Server) handleRawUpload(w http.ResponseWriter, r *http.Request, encodedFilename string) {
	requestStart := time.Now()
	
	// SECURITY: Enforce 10GB upload limit
	const MaxUploadSize = 10 << 30 // 10GB
	if r.ContentLength > MaxUploadSize {
		http.Error(w, "file too large", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, MaxUploadSize)
	
	// Decode filename from URL encoding
	filename, err := url.QueryUnescape(encodedFilename)
	if err != nil {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}
	
	// Sanitize filename to prevent directory traversal
	name := filepath.Base(filename)
	if name == "." || name == ".." || name == "" {
		http.Error(w, "invalid filename", http.StatusBadRequest)
		return
	}
	
	// Ensure upload dir exists
	dest := s.UploadDir
	if dest == "" {
		dest = "."
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}
	
	// PRODUCTION FIX #1: Prevent file collisions
	outPath := findUniqueFilename(dest, name)
	actualFilename := filepath.Base(outPath)
	
	// Create file with secure permissions
	f, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		log.Printf("Failed to create file %s: %v", actualFilename, err)
		http.Error(w, "disk error", http.StatusInternalServerError)
		return
	}
	
	// PRODUCTION FIX #2: Clean up zombie files on disconnect
	success := false
	defer func() {
		f.Close()
		if !success {
			os.Remove(outPath)
			log.Printf("Upload canceled/failed: deleted incomplete file %s", actualFilename)
		}
	}()
	
	// CRITICAL OPTIMIZATION: Pre-allocate disk space
	// This prevents file fragmentation and reduces metadata updates
	// Massive performance gain for large files on SSDs
	if r.ContentLength > 0 {
		if err := f.Truncate(r.ContentLength); err != nil {
			log.Printf("Failed to pre-allocate space for %s: %v", actualFilename, err)
			// Non-fatal, continue anyway
		}
	}
	
	// Get buffer from pool (zero allocation)
	bufPtr := bufferPool.Get().(*[]byte)
	defer bufferPool.Put(bufPtr)
	buf := *bufPtr
	
	// ZERO-PARSING STREAM: Network → Disk
	// No boundary scanning, no base64 decoding, pure binary transfer
	n, err := io.CopyBuffer(f, r.Body, buf)
	if err != nil {
		log.Printf("Upload stream failed for %s: %v", actualFilename, err)
		http.Error(w, "stream error", http.StatusInternalServerError)
		return
	}
	
	// Mark success before defer runs
	success = true
	
	// Calculate transfer speed
	duration := time.Since(requestStart).Seconds()
	mbps := 0.0
	if duration > 0 {
		mbps = (float64(n) * 8) / (duration * 1_000_000)
	}
	
	log.Printf("%s, %s received in %.2fs (%.1f Mbps)", actualFilename, formatBytes(n), duration, mbps)
	
	// Send success response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"success":true,"filename":"%s","size":%d}`, actualFilename, n)
}

// formatBytes formats bytes into human-readable string
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// Shutdown stops the server.
func (s *Server) Shutdown() error {
	if s.httpServer == nil {
		return nil
	}
	if s.advertiser != nil {
		s.advertiser.Close()
	}
	return s.httpServer.Close()
}