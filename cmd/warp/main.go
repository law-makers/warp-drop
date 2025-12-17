package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/zulfikawr/warp-drop/internal/client"
	"github.com/zulfikawr/warp-drop/internal/crypto"
	"github.com/zulfikawr/warp-drop/internal/server"
	"github.com/zulfikawr/warp-drop/internal/ui"
)

func main() {
	log.SetFlags(0)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	switch sub {
	case "send":
		sendCmd(os.Args[2:])
	case "receive":
		receiveCmd(os.Args[2:])
	case "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println("Usage:\n  warp send [flags] <path>\n  warp receive [flags] <url>")
}

func sendCmd(args []string) {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	port := fs.Int("port", 0, "specific port")
	fs.IntVar(port, "p", 0, "")
	noQR := fs.Bool("no-qr", false, "disable QR")
	iface := fs.String("interface", "", "network interface")
	fs.StringVar(iface, "i", "", "")
	verbose := fs.Bool("verbose", false, "verbose logging")
	fs.BoolVar(verbose, "v", false, "")
	fs.Parse(args)
	if fs.NArg() < 1 {
		log.Fatal("send requires a path")
	}
	path := fs.Arg(0)

	tok, err := crypto.GenerateToken(nil)
	if err != nil { log.Fatal(err) }
	srv := &server.Server{InterfaceName: *iface, Token: tok, SrcPath: path}
	url, err := srv.Start()
	if err != nil { log.Fatal(err) }
	defer srv.Shutdown()
	fmt.Printf("> Serving '%s'\n> Token: %s\n\n", path, tok)
	if !*noQR {
		_ = ui.PrintQR(url)
	}
	fmt.Printf("Or run: warp receive %s\n", url)
	select {} // block until interrupted
}

func receiveCmd(args []string) {
	fs := flag.NewFlagSet("receive", flag.ExitOnError)
	out := fs.String("output", "", "output path")
	fs.StringVar(out, "o", "", "")
	force := fs.Bool("force", false, "overwrite existing")
	fs.BoolVar(force, "f", false, "")
	verbose := fs.Bool("verbose", false, "verbose logging")
	fs.BoolVar(verbose, "v", false, "")
	fs.Parse(args)
	if fs.NArg() < 1 {
		log.Fatal("receive requires a URL")
	}
	url := fs.Arg(0)
	file, err := client.Receive(url, *out, *force, os.Stdout)
	if err != nil { log.Fatal(err) }
	fmt.Printf("\nSaved to %s\n", file)
}
