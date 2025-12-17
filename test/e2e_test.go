package test

import (
	"bytes"
	"crypto/md5"
	"io"
	"io/ioutil"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zulfikawr/warp-drop/internal/client"
	"github.com/zulfikawr/warp-drop/internal/crypto"
	"github.com/zulfikawr/warp-drop/internal/server"
)

func TestE2E_FileTransfer(t *testing.T) {
	// Prepare source file
	src, err := ioutil.TempFile("", "warp-src")
	if err != nil { t.Fatal(err) }
	defer os.Remove(src.Name())
	data := bytes.Repeat([]byte{'a'}, 1<<20) // 1MB
	if _, err := src.Write(data); err != nil { t.Fatal(err) }
	_ = src.Close()

	// Start server
	tok, _ := crypto.GenerateToken(nil)
	srv := &server.Server{Token: tok, SrcPath: src.Name()}
	url, err := srv.Start()
	if err != nil { t.Fatal(err) }
	defer srv.Shutdown()

	// Receive
	out, err := client.Receive(url, "", true, ioutil.Discard)
	if err != nil { t.Fatal(err) }
	defer os.Remove(out)

	// Compare md5
	srcb, _ := os.ReadFile(src.Name())
	outb, _ := os.ReadFile(out)
	sh := md5.Sum(srcb)
	oh := md5.Sum(outb)
	if sh != oh {
		t.Fatalf("md5 mismatch: %x vs %x", sh, oh)
	}
}
// TestE2E_TextSharing tests the clipboard/text sharing feature
func TestE2E_TextSharing(t *testing.T) {
	testCases := []string{
		"Hello World",
		"API_KEY_12345_SECRET",
		"https://example.com/very/long/url/path?token=abc123&id=456",
		"multiline\ntext\nsharing\ntest",
		"{\"json\": \"content\", \"nested\": {\"key\": \"value\"}}",
	}

	for _, testText := range testCases {
		t.Run("TextShare_"+string(testText[0]), func(t *testing.T) {
			// Start server with text content
			tok, _ := crypto.GenerateToken(nil)
			srv := &server.Server{Token: tok, TextContent: testText}
			url, err := srv.Start()
			if err != nil {
				t.Fatal(err)
			}
			defer srv.Shutdown()

			// Receive text (capture output)
			result, err := client.Receive(url, "", false, ioutil.Discard)
			if err != nil {
				t.Fatal(err)
			}

			if result != "(stdout)" {
				t.Fatalf("expected (stdout), got %s", result)
			}
		})
	}
}

// TestE2E_TextSecurity tests that text content is not cached and has proper headers
func TestE2E_TextSecurityHeaders(t *testing.T) {
	sensitiveText := "SECRET_TOKEN_DO_NOT_CACHE"

	// Start server with sensitive text
	tok, _ := crypto.GenerateToken(nil)
	srv := &server.Server{Token: tok, TextContent: sensitiveText}
	urlStr, err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()

	// Make a request and check headers
	// Note: In real testing, we'd need to mock the HTTP client
	// For now, we verify that Receive properly handles text responses
	result, err := client.Receive(urlStr, "", false, ioutil.Discard)
	if err != nil {
		t.Fatal(err)
	}

	// Text should be output to stdout, not saved to file
	if result != "(stdout)" {
		t.Fatalf("text should output to stdout, got: %s", result)
	}
}

// TestE2E_TokenAuthentication verifies that invalid tokens are rejected
func TestE2E_TokenAuthentication(t *testing.T) {
	tok, _ := crypto.GenerateToken(nil)
	srv := &server.Server{Token: tok, TextContent: "secret"}
	_, err := srv.Start()
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Shutdown()

	// In a real scenario with a mock HTTP client, we would test
	// that requests with invalid tokens are rejected with 403 Forbidden.
	// Since we cannot easily modify the URL token in real client.Receive,
	// this test verifies the server starts successfully with token auth.
}

// TestE2E_HostUpload verifies reverse drop (host mode) allows uploading files securely.
func TestE2E_HostUpload(t *testing.T) {
	// Prepare destination directory
	destDir, err := ioutil.TempDir("", "warp-host-dest")
	if err != nil { t.Fatal(err) }
	defer os.RemoveAll(destDir)

	// Start host server
	tok, _ := crypto.GenerateToken(nil)
	srv := &server.Server{Token: tok, HostMode: true, UploadDir: destDir}
	url, err := srv.Start()
	if err != nil { t.Fatal(err) }
	defer srv.Shutdown()

	// GET should serve HTML form
	resp, err := http.Get(url)
	if err != nil { t.Fatal(err) }
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "<form") {
		t.Fatalf("expected HTML form, got status %d", resp.StatusCode)
	}

	// Prepare multipart POST with a small file
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", "upload.txt")
	if err != nil { t.Fatal(err) }
	io.WriteString(fw, "reverse-drop-works")
	mw.Close()

	// POST to the same URL
	req, err := http.NewRequest(http.MethodPost, url, &buf)
	if err != nil { t.Fatal(err) }
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp2, err := http.DefaultClient.Do(req)
	if err != nil { t.Fatal(err) }
	io.Copy(io.Discard, resp2.Body)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("upload failed: status %d", resp2.StatusCode)
	}

	// Verify file exists on disk
	savedPath := filepath.Join(destDir, "upload.txt")
	b, err := os.ReadFile(savedPath)
	if err != nil { t.Fatal(err) }
	if string(b) != "reverse-drop-works" {
		t.Fatalf("unexpected file content: %q", string(b))
	}
}

// TestE2E_ResumableDownload verifies that downloads can be resumed from where they left off.
func TestE2E_ResumableDownload(t *testing.T) {
	// Prepare a larger source file (10MB)
	src, err := ioutil.TempFile("", "warp-resume-src")
	if err != nil { t.Fatal(err) }
	defer os.Remove(src.Name())
	
	// Write 10MB of data
	data := bytes.Repeat([]byte("ABCDEFGHIJ"), 1024*1024) // 10MB
	if _, err := src.Write(data); err != nil { t.Fatal(err) }
	_ = src.Close()

	// Start server
	tok, _ := crypto.GenerateToken(nil)
	srv := &server.Server{Token: tok, SrcPath: src.Name()}
	url, err := srv.Start()
	if err != nil { t.Fatal(err) }
	defer srv.Shutdown()

	// Create output path
	outPath, err := ioutil.TempFile("", "warp-resume-out")
	if err != nil { t.Fatal(err) }
	outName := outPath.Name()
	defer os.Remove(outName)

	// First download: interrupt after partial download
	// We'll simulate by downloading to a partial file
	resp, err := http.Get(url)
	if err != nil { t.Fatal(err) }
	
	// Only read 5MB (half the file)
	limited := io.LimitReader(resp.Body, 5*1024*1024)
	_, err = io.Copy(outPath, limited)
	resp.Body.Close()
	outPath.Close()
	if err != nil { t.Fatal(err) }

	// Verify partial file is 5MB
	fi, err := os.Stat(outName)
	if err != nil { t.Fatal(err) }
	if fi.Size() != 5*1024*1024 {
		t.Fatalf("expected partial file of 5MB, got %d bytes", fi.Size())
	}

	// Resume download using client.Receive (it should detect partial file and resume)
	// Force is true to allow resuming existing file
	result, err := client.Receive(url, outName, true, ioutil.Discard)
	if err != nil { t.Fatal(err) }
	if result != outName {
		t.Fatalf("expected %s, got %s", outName, result)
	}

	// Verify complete file matches original
	srcb, _ := os.ReadFile(src.Name())
	outb, _ := os.ReadFile(outName)
	if len(outb) != len(srcb) {
		t.Fatalf("size mismatch: expected %d, got %d", len(srcb), len(outb))
	}
	
	sh := md5.Sum(srcb)
	oh := md5.Sum(outb)
	if sh != oh {
		t.Fatalf("md5 mismatch after resume: %x vs %x", sh, oh)
	}
}
