package test

import (
	"bytes"
	"crypto/md5"
	"io/ioutil"
	"os"
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
