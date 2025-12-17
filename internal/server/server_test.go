package server

import (
	"io/ioutil"
	"net/http"
	"os"
	"testing"

	"github.com/zulfikawr/warp-drop/internal/crypto"
)

func TestServerValidAndInvalidToken(t *testing.T) {
	tmpFile, err := ioutil.TempFile("", "warp-drop-test")
	if err != nil { t.Fatal(err) }
	defer os.Remove(tmpFile.Name())
	_, _ = tmpFile.Write([]byte("hello"))
	_ = tmpFile.Close()

	tok, _ := crypto.GenerateToken(nil)
	s := &Server{Token: tok, SrcPath: tmpFile.Name()}
	url, err := s.Start()
	if err != nil { t.Fatal(err) }
	defer s.Shutdown()

	// Valid token
	resp, err := http.Get(url)
	if err != nil { t.Fatal(err) }
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	// Invalid token
	resp2, err := http.Get(url+"x")
	if err != nil { t.Fatal(err) }
	if resp2.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp2.StatusCode)
	}
	resp2.Body.Close()
}
