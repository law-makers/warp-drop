package main

import (
	"flag"
	"fmt"
	"io"
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
	fmt.Println("Usage:\n  warp send [flags] <path>\n  warp send --text <text>\n  warp send --stdin < file\n  warp receive [flags] <url>")
}

func sendCmd(args []string) {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	port := fs.Int("port", 0, "specific port")
	fs.IntVar(port, "p", 0, "")
	noQR := fs.Bool("no-qr", false, "disable QR")
	iface := fs.String("interface", "", "network interface")
	fs.StringVar(iface, "i", "", "")
	text := fs.String("text", "", "send text instead of file")
	stdin := fs.Bool("stdin", false, "read from stdin")
	verbose := fs.Bool("verbose", false, "verbose logging")
	fs.BoolVar(verbose, "v", false, "")
	fs.Parse(args)

	tok, err := crypto.GenerateToken(nil)
	if err != nil { log.Fatal(err) }

	var srv *server.Server

	// Handle text sharing
	if *text != "" {
		srv = &server.Server{InterfaceName: *iface, Token: tok, TextContent: *text}
	} else if *stdin {
		// Read from stdin
		data, err := io.ReadAll(os.Stdin)
		if err != nil { log.Fatal(err) }
		srv = &server.Server{InterfaceName: *iface, Token: tok, TextContent: string(data)}
	} else {
		// Handle file/directory
		if fs.NArg() < 1 {
			log.Fatal("send requires a path, --text, or --stdin")
		}
		path := fs.Arg(0)
		srv = &server.Server{InterfaceName: *iface, Token: tok, SrcPath: path}
	}

	url, err := srv.Start()
	if err != nil { log.Fatal(err) }
	defer srv.Shutdown()

	// Display what we're serving
	if srv.TextContent != "" {
		fmt.Printf("> Serving text (%d bytes)\n", len(srv.TextContent))
	} else {
		fmt.Printf("> Serving '%s'\n", srv.SrcPath)
	}
	fmt.Printf("> Token: %s\n\n", tok)

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
	if file == "(stdout)" {
		// Text was output to stdout, just print newline
		fmt.Println()
	} else {
		fmt.Printf("\nSaved to %s\n", file)
	}
}
