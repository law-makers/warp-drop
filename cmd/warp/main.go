package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/zulfikawr/warp/internal/client"
	"github.com/zulfikawr/warp/internal/crypto"
	"github.com/zulfikawr/warp/internal/discovery"
	"github.com/zulfikawr/warp/internal/server"
	"github.com/zulfikawr/warp/internal/ui"
)

// ANSI colors for readable help output (toggled via --no-color / NO_COLOR)
var (
	cReset   string
	cBold    string
	cDim     string
	cGreen   string
	cYellow  string
	cMagenta string
)

func setColorsEnabled(enabled bool) {
	if !enabled {
		cReset, cBold, cDim, cGreen, cYellow, cMagenta = "", "", "", "", "", ""
		return
	}
	cReset = "\033[0m"
	cBold = "\033[1m"
	cDim = "\033[2m"
	cGreen = "\033[32m"
	cYellow = "\033[33m"
	cMagenta = "\033[35m"
}

// filter out global flags that subcommands don't recognize
func filterGlobalFlags(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--no-color" {
			continue
		}
		out = append(out, a)
	}
	return out
}

func main() {
	log.SetFlags(0)
	// Determine color usage from env and global flag
	enableColors := os.Getenv("NO_COLOR") == ""
	for _, a := range os.Args[1:] {
		if a == "--no-color" {
			enableColors = false
			break
		}
	}
	setColorsEnabled(enableColors)
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	sub := os.Args[1]
	switch sub {
	case "send":
		sendCmd(filterGlobalFlags(os.Args[2:]))
	case "host":
		hostCmd(filterGlobalFlags(os.Args[2:]))
	case "receive":
		receiveCmd(filterGlobalFlags(os.Args[2:]))
	case "search":
		searchCmd(filterGlobalFlags(os.Args[2:]))
	case "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println("     ")
	fmt.Println("██   ██  ▀▀█▄ ████▄ ████▄")
	fmt.Println("██ █ ██ ▄█▀██ ██ ▀▀ ██ ██")
	fmt.Println(" ██▀██  ▀█▄██ ██    ████▀")
	fmt.Println("                    ██    ")
	fmt.Println("                    ▀▀    ")
	fmt.Println(cDim + "a quick file and text transfer" + cReset)
	fmt.Println()

	fmt.Println(cBold + "Usage:" + cReset)
	fmt.Println("  " + cGreen + "warp send" + cReset + " [flags] <path>")
	fmt.Println("  " + cGreen + "warp send" + cReset + " --text <text>")
	fmt.Println("  " + cGreen + "warp send" + cReset + " --stdin < file")
	fmt.Println("  " + cGreen + "warp host" + cReset + " [flags]")
	fmt.Println("  " + cGreen + "warp receive" + cReset + " [flags] <url>")
	fmt.Println("  " + cGreen + "warp search" + cReset + " [flags]")
	fmt.Println()

	fmt.Println(cBold + "Commands:" + cReset)
	fmt.Println("  " + cMagenta + "send" + cReset + "  Share a file, directory, or text snippet")
	fmt.Println("\t" + cYellow + "-p, --port" + cReset + "        choose specific port (default random)")
	fmt.Println("\t" + cYellow + "-i, --interface" + cReset + "   bind to a specific network interface")
	fmt.Println("\t" + cYellow + "--text string" + cReset + "     send a text snippet instead of a file")
	fmt.Println("\t" + cYellow + "--stdin" + cReset + "           read text from stdin")
	fmt.Println("\t" + cYellow + "--no-qr" + cReset + "           skip printing the QR code")
	fmt.Println()
	fmt.Println("  " + cMagenta + "host" + cReset + "  Receive uploads into a directory you control")
	fmt.Println("\t" + cYellow + "-i, --interface" + cReset + "   bind to a specific network interface")
	fmt.Println("\t" + cYellow + "-d, --dest" + cReset + "        destination directory for uploads (default .)")
	fmt.Println("\t" + cYellow + "--no-qr" + cReset + "           skip printing the QR code")
	fmt.Println()
	fmt.Println("  " + cMagenta + "receive" + cReset + "  Download from a warp URL")
	fmt.Println("\t" + cYellow + "-o, --output" + cReset + "      write to a specific file or directory")
	fmt.Println("\t" + cYellow + "-f, --force" + cReset + "       overwrite existing files")
	fmt.Println()
	fmt.Println("  " + cMagenta + "search" + cReset + "   Discover nearby warp hosts via mDNS")
	fmt.Println("\t" + cYellow + "--timeout" + cReset + "          duration to wait for discovery (default 3s)")
	fmt.Println()

	fmt.Println(cBold + "Examples:" + cReset)
	fmt.Println("  " + cGreen + "warp send" + cReset + " ./photo.jpg " + cDim + "		    # Share a file" + cReset)
	fmt.Println("  " + cGreen + "warp send" + cReset + " --text \"hello\" " + cDim + "	            # Share text" + cReset)
	fmt.Println("  " + cGreen + "warp host" + cReset + " -d uploads " + cDim + "		            # Save uploads to dir" + cReset)
	fmt.Println("  " + cGreen + "warp search" + cReset + " " + cDim + "				    # Discover hosts" + cReset)
	fmt.Println("  " + cGreen + "warp receive" + cReset + " http://hostname:port/<token> " + cDim + "# Download" + cReset)
	fmt.Println()
	fmt.Println(cDim + "Use \"warp <command> -h\" for command-specific help." + cReset)
}

func sendHelp() {
	fmt.Println(cBold + cGreen + "warp send" + cReset + " - Share a file, directory, or text snippet")
	fmt.Println()
	fmt.Println(cBold + "Usage:" + cReset)
	fmt.Println("  " + cGreen + "warp send" + cReset + " [flags] <path>")
	fmt.Println("  " + cGreen + "warp send" + cReset + " --text <text>")
	fmt.Println("  " + cGreen + "warp send" + cReset + " --stdin < file")
	fmt.Println()
	fmt.Println(cBold + "Description:" + cReset)
	fmt.Println("  Start a server and share a file, directory, or text with another device.")
	fmt.Println("  The recipient can download using the generated URL or token.")
	fmt.Println()
	fmt.Println(cBold + "Flags:" + cReset)
	fmt.Println("  " + cYellow + "-p, --port" + cReset + "        choose specific port (default: random)")
	fmt.Println("  " + cYellow + "-i, --interface" + cReset + "   bind to a specific network interface")
	fmt.Println("  " + cYellow + "--text string" + cReset + "     send a text snippet instead of a file")
	fmt.Println("  " + cYellow + "--stdin" + cReset + "           read text content from stdin")
	fmt.Println("  " + cYellow + "--no-qr" + cReset + "           skip printing the QR code")
	fmt.Println("  " + cYellow + "-v, --verbose" + cReset + "     verbose logging")
	fmt.Println()
	fmt.Println(cBold + "Examples:" + cReset)
	fmt.Println("  " + cGreen + "warp send" + cReset + " ./photo.jpg              " + cDim + "# Share a file" + cReset)
	fmt.Println("  " + cGreen + "warp send" + cReset + " ./documents/             " + cDim + "# Share a directory" + cReset)
	fmt.Println("  " + cGreen + "warp send" + cReset + " --text \"hello world\"     " + cDim + "# Share text" + cReset)
	fmt.Println("  echo \"hello\" | " + cGreen + "warp send" + cReset + " --stdin   " + cDim + "# Read from stdin" + cReset)
	fmt.Println("  " + cGreen + "warp send" + cReset + " -p 8080 ./file.zip       " + cDim + "# Use specific port" + cReset)
}

func hostHelp() {
	fmt.Println(cBold + cGreen + "warp host" + cReset + " - Receive uploads into a directory you control")
	fmt.Println()
	fmt.Println(cBold + "Usage:" + cReset)
	fmt.Println("  " + cGreen + "warp host" + cReset + " [flags]")
	fmt.Println()
	fmt.Println(cBold + "Description:" + cReset)
	fmt.Println("  Start an upload server and receive files from other devices.")
	fmt.Println("  Uploaded files are saved to the specified directory.")
	fmt.Println()
	fmt.Println(cBold + "Flags:" + cReset)
	fmt.Println("  " + cYellow + "-i, --interface" + cReset + "   bind to a specific network interface")
	fmt.Println("  " + cYellow + "-d, --dest" + cReset + "        destination directory for uploads (default: .)")
	fmt.Println("  " + cYellow + "--no-qr" + cReset + "           skip printing the QR code")
	fmt.Println("  " + cYellow + "-v, --verbose" + cReset + "     verbose logging")
	fmt.Println()
	fmt.Println(cBold + "Examples:" + cReset)
	fmt.Println("  " + cGreen + "warp host" + cReset + "                          " + cDim + "# Accept uploads to current directory" + cReset)
	fmt.Println("  " + cGreen + "warp host" + cReset + " -d ./uploads             " + cDim + "# Save uploads to ./uploads" + cReset)
	fmt.Println("  " + cGreen + "warp host" + cReset + " -d ./downloads -i eth0   " + cDim + "# Bind to specific interface" + cReset)
}

func receiveHelp() {
	fmt.Println(cBold + cGreen + "warp receive" + cReset + " - Download from a warp URL")
	fmt.Println()
	fmt.Println(cBold + "Usage:" + cReset)
	fmt.Println("  " + cGreen + "warp receive" + cReset + " [flags] <url>")
	fmt.Println()
	fmt.Println(cBold + "Description:" + cReset)
	fmt.Println("  Connect to a warp server and download the shared file or text.")
	fmt.Println("  Downloaded files are saved to the current directory or specified path.")
	fmt.Println("  Text content is printed to stdout by default.")
	fmt.Println()
	fmt.Println(cBold + "Flags:" + cReset)
	fmt.Println("  " + cYellow + "-o, --output" + cReset + "      write to a specific file or directory")
	fmt.Println("  " + cYellow + "-f, --force" + cReset + "       overwrite existing files without prompting")
	fmt.Println("  " + cYellow + "-v, --verbose" + cReset + "     verbose logging")
	fmt.Println()
	fmt.Println(cBold + "Examples:" + cReset)
	fmt.Println("  " + cGreen + "warp receive" + cReset + " http://host:port/d/token                " + cDim + "# Download file" + cReset)
	fmt.Println("  " + cGreen + "warp receive" + cReset + " http://host:port/d/token -o myfile.zip  " + cDim + "# Save with custom name" + cReset)
	fmt.Println("  " + cGreen + "warp receive" + cReset + " http://host:port/d/token -d downloads   " + cDim + "# Save to directory" + cReset)
	fmt.Println("  " + cGreen + "warp receive" + cReset + " http://host:port/t/token                " + cDim + "# Print text to stdout" + cReset)
}

func searchHelp() {
	fmt.Println(cBold + cGreen + "warp search" + cReset + " - Discover nearby warp hosts via mDNS")
	fmt.Println()
	fmt.Println(cBold + "Usage:" + cReset)
	fmt.Println("  " + cGreen + "warp search" + cReset + " [flags]")
	fmt.Println()
	fmt.Println(cBold + "Description:" + cReset)
	fmt.Println("  Search for warp servers on your local network using mDNS (Bonjour).")
	fmt.Println("  Displays discovered hosts with their names, modes, and URLs.")
	fmt.Println()
	fmt.Println(cBold + "Flags:" + cReset)
	fmt.Println("  " + cYellow + "--timeout" + cReset + "          duration to wait for discovery (default: 3s)")
	fmt.Println()
	fmt.Println(cBold + "Examples:" + cReset)
	fmt.Println("  " + cGreen + "warp search" + cReset + "                        " + cDim + "# Search with default 3s timeout" + cReset)
	fmt.Println("  " + cGreen + "warp search" + cReset + " --timeout 5s           " + cDim + "# Search for 5 seconds" + cReset)
	fmt.Println("  " + cGreen + "warp search" + cReset + " --timeout 100ms        " + cDim + "# Quick search" + cReset)
}

func sendCmd(args []string) {
	fs := flag.NewFlagSet("send", flag.ExitOnError)
	fs.Usage = sendHelp
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
	fs.Usage = receiveHelp
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

func hostCmd(args []string) {
	fs := flag.NewFlagSet("host", flag.ExitOnError)
	fs.Usage = hostHelp
	iface := fs.String("interface", "", "network interface")
	fs.StringVar(iface, "i", "", "")
	dest := fs.String("dest", ".", "destination directory for uploads")
	fs.StringVar(dest, "d", ".", "")
	noQR := fs.Bool("no-qr", false, "disable QR")
	verbose := fs.Bool("verbose", false, "verbose logging")
	fs.BoolVar(verbose, "v", false, "")
	fs.Parse(args)

	// Ensure destination exists
	if err := os.MkdirAll(*dest, 0o755); err != nil {
		log.Fatal(err)
	}

	tok, err := crypto.GenerateToken(nil)
	if err != nil { log.Fatal(err) }
	srv := &server.Server{InterfaceName: *iface, Token: tok, HostMode: true, UploadDir: *dest}
	url, err := srv.Start()
	if err != nil { log.Fatal(err) }
	defer srv.Shutdown()

	fmt.Printf("> Hosting uploads to '%s'\n> Token: %s\n\n", *dest, tok)
	if !*noQR {
		_ = ui.PrintQR(url)
	}
	fmt.Printf("Open this on another device to upload:\n%s\n", url)
	select {}
}

func searchCmd(args []string) {
	fs := flag.NewFlagSet("search", flag.ExitOnError)
	fs.Usage = searchHelp
	timeout := fs.Duration("timeout", 3*time.Second, "discovery timeout")
	fs.Parse(args)

	services, err := discovery.Browse(context.Background(), *timeout)
	if err != nil {
		log.Fatal(err)
	}

	if len(services) == 0 {
		fmt.Println("No warp hosts found")
		return
	}

	fmt.Println("Discovered hosts:")
	for _, svc := range services {
		fmt.Printf("- %s [%s] %s\n", svc.Name, svc.Mode, svc.URL)
	}
}
