package client

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"
)

// Receive downloads from url to outputPath. If outputPath is empty, derive from headers or URL.
// For text content (Content-Type: text/plain), outputs to stdout instead of saving to a file.
// Supports resumable downloads via HTTP Range headers if the file already partially exists.
func Receive(url string, outputPath string, force bool, progress io.Writer) (string, error) {
	// First, make a HEAD request or GET to determine filename and check for existing partial file
	var startByte int64 = 0
	var existingSize int64 = 0
	
	// Try initial request to get headers
	resp, err := http.Get(url)
	if err != nil { return "", err }
	
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		resp.Body.Close()
		return "", fmt.Errorf("http status %d", resp.StatusCode)
	}

	// Check if this is text content (text/plain without attachment disposition)
	contentType := resp.Header.Get("Content-Type")
	disposition := resp.Header.Get("Content-Disposition")
	isTextContent := strings.HasPrefix(contentType, "text/plain") && disposition == ""

	if isTextContent {
		// Output text to stdout
		_, err := io.Copy(os.Stdout, resp.Body)
		resp.Body.Close()
		if err != nil { return "", err }
		return "(stdout)", nil
	}

	name := filenameFromResponse(resp)
	if name == "" {
		name = path.Base(resp.Request.URL.Path)
		if name == "" { name = "download.bin" }
	}
	if outputPath == "" {
		outputPath = name
	}
	
	totalSize := resp.ContentLength
	resp.Body.Close()
	
	// Check if file already exists and can be resumed
	var f *os.File
	if fi, err := os.Stat(outputPath); err == nil {
		existingSize = fi.Size()
		if !force && existingSize > 0 && existingSize < totalSize {
			// File exists and is incomplete - try to resume
			startByte = existingSize
			f, err = os.OpenFile(outputPath, os.O_WRONLY|os.O_APPEND, 0o600)
			if err != nil { return "", err }
		} else if !force {
			return "", errors.New("destination exists; use --force to overwrite")
		} else {
			// Force overwrite
			f, err = os.Create(outputPath)
			if err != nil { return "", err }
		}
	} else {
		// File doesn't exist - create new
		f, err = os.Create(outputPath)
		if err != nil { return "", err }
	}
	defer f.Close()
	
	// Make the actual download request with Range header if resuming
	var downloadResp *http.Response
	if startByte > 0 {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil { return "", err }
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startByte))
		downloadResp, err = http.DefaultClient.Do(req)
		if err != nil { return "", err }
		defer downloadResp.Body.Close()
		
		if downloadResp.StatusCode != http.StatusPartialContent {
			// Server doesn't support resume, start over
			f.Close()
			f, err = os.Create(outputPath)
			if err != nil { return "", err }
			defer f.Close()
			startByte = 0
			downloadResp.Body.Close()
			downloadResp, err = http.Get(url)
			if err != nil { return "", err }
			defer downloadResp.Body.Close()
		}
	} else {
		downloadResp, err = http.Get(url)
		if err != nil { return "", err }
		defer downloadResp.Body.Close()
	}
	
	var src io.Reader = downloadResp.Body
	if progress != nil {
		// Start progress tracking from existing bytes if resuming
		src = &progressReader{r: downloadResp.Body, total: totalSize, read: startByte, out: progress, start: time.Now()}
	}
	if _, err := io.Copy(f, src); err != nil { return "", err }
	return outputPath, nil
}

func filenameFromResponse(resp *http.Response) string {
	cd := resp.Header.Get("Content-Disposition")
	if cd == "" { return "" }
	// simplistic parsing: attachment; filename="name"
	parts := strings.Split(cd, ";")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if strings.HasPrefix(strings.ToLower(p), "filename=") {
			v := strings.TrimPrefix(p, "filename=")
			v = strings.Trim(v, "\"")
			return v
		}
	}
	return ""
}

// progressReader is a lightweight progress wrapper.
type progressReader struct {
	r     io.Reader
	total int64
	read  int64
	out   io.Writer
	start time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.read += int64(n)
	if p.total > 0 && p.out != nil {
		pct := float64(p.read) / float64(p.total) * 100.0
		elapsed := time.Since(p.start).Seconds()
		var mbps float64
		if elapsed > 0 {
			// Convert bytes to megabits: (bytes * 8) / (1_000_000 bits per megabit)
			mbps = (float64(p.read) * 8) / (elapsed * 1_000_000)
		}
		fmt.Fprintf(p.out, "\r[%-20s] %3.0f%% | %5.1f Mbps", bar(pct), pct, mbps)
	}
	return n, err
}

func bar(pct float64) string {
	filled := int(pct / 5) // 20 slots
	if filled < 0 { filled = 0 }
	if filled > 20 { filled = 20 }
	return strings.Repeat("=", filled) + strings.Repeat(" ", 20-filled)
}
