package server

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/zulfikawr/warp-drop/internal/network"
	"github.com/zulfikawr/warp-drop/internal/protocol"
)

type Server struct {
	InterfaceName string
	Token         string
	SrcPath       string
	TextContent   string // If set, serves text instead of file
	ip            net.IP
	Port          int
	httpServer    *http.Server
}

// Start initializes and starts the HTTP server. It returns the accessible URL.
func (s *Server) Start() (string, error) {
	ip, err := network.DiscoverLANIP(s.InterfaceName)
	if err != nil {
		return "", err
	}
	s.ip = ip

	mux := http.NewServeMux()
	mux.HandleFunc(protocol.PathPrefix, s.handleDownload)

	s.httpServer = &http.Server{
		ReadTimeout:  protocol.ReadTimeout,
		WriteTimeout: protocol.WriteTimeout,
		Handler:      mux,
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("%s:0", ip.String()))
	if err != nil {
		return "", err
	}
	addr := ln.Addr().String() // ip:port
	parts := strings.Split(addr, ":")
	if len(parts) < 2 {
		_ = ln.Close()
		return "", fmt.Errorf("unexpected listener addr: %s", addr)
	}
	portStr := parts[len(parts)-1]
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	s.Port = port

	go func() {
		_ = s.httpServer.Serve(ln)
	}()

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
	http.ServeFile(w, r, s.SrcPath)
}

// Shutdown stops the server.
func (s *Server) Shutdown() error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Close()
}
