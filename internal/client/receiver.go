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
func Receive(url string, outputPath string, force bool, progress io.Writer) (string, error) {
	resp, err := http.Get(url)
	if err != nil { return "", err }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("http status %d", resp.StatusCode)
	}

	// Check if this is text content (text/plain without attachment disposition)
	contentType := resp.Header.Get("Content-Type")
	disposition := resp.Header.Get("Content-Disposition")
	isTextContent := strings.HasPrefix(contentType, "text/plain") && disposition == ""

	if isTextContent {
		// Output text to stdout
		_, err := io.Copy(os.Stdout, resp.Body)
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
	if !force {
		if _, err := os.Stat(outputPath); err == nil {
			return "", errors.New("destination exists; use --force to overwrite")
		}
	}
	f, err := os.Create(outputPath)
	if err != nil { return "", err }
	defer f.Close()
	var src io.Reader = resp.Body
	if progress != nil {
		src = &progressReader{r: resp.Body, total: resp.ContentLength, out: progress, start: time.Now()}
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
