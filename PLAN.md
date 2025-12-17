Here is the **Definitive Project Plan** for **Warp-Drop**. This version is fully fleshed out with a concrete directory structure, precise CLI usage definitions, and a "No Stone Unturned" testing strategy.

---

# Project: Warp-Drop (Master Plan)

**Goal:** A standalone binary for secure, high-speed local file transfer.
**Constraint:** Zero external dependencies (except QR generation), strict Go Standard Library preference.

## 1. Project Structure (File Tree)

We will follow the Standard Go Project Layout to ensure the code is maintainable and testable.

```text
warp-drop/
├── cmd/
│   └── warp/
│       └── main.go           # Entry point (parses flags, routes commands)
├── internal/
│   ├── crypto/
│   │   ├── token.go          # Generates secure session tokens
│   │   └── token_test.go     # Tests entropy and validation
│   ├── network/
│   │   ├── ip.go             # LAN IP discovery logic
│   │   └── ip_test.go        # Tests IP filtering logic
│   ├── protocol/
│   │   ├── handshake.go      # Shared constants (headers, timeout values)
│   │   └── handshake_test.go
│   ├── server/
│   │   ├── http.go           # The Sender logic (Server struct)
│   │   ├── zip.go            # Streaming zip logic
│   │   └── server_test.go    # Tests handlers, auth, and file streaming
│   ├── client/
│   │   ├── receiver.go       # The Receiver logic (Client struct)
│   │   └── receiver_test.go  # Tests connection and file saving
│   └── ui/
│       ├── qr.go             # QR code rendering (ASCII/ANSI)
│       ├── progress.go       # Progress bar (io.Reader wrapper)
│       └── ui_test.go        # Visual output tests
├── test/
│   └── e2e_test.go           # End-to-End integration test (Spin up server -> download -> verify)
├── go.mod                    # Dependency tracking
├── go.sum
└── README.md

```

---

## 2. CLI Usage Specification

The app has two primary modes: `send` and `receive`.

### Global Flags

* `--verbose, -v`: Enable debug logging (IP interfaces found, detailed error traces).
* `--help, -h`: Show usage instructions.

### Command: `warp send`

**Usage:** `warp send [flags] <path-to-file-or-folder>`

**Flags:**

* `--port, -p`: Force a specific port (default: random free port).
* `--no-qr`: Disable QR code output (useful for script/headless mode).
* `--interface, -i`: Force a specific network interface (e.g., `eth0`, `wlan0`).

**Example Output:**

```text
$ warp send ./vacation_photos
> Scanning network interfaces... Found 192.168.1.15
> Serving './vacation_photos' (Directory - will be zipped on fly)
> Token: 8f9a2b...

[ QR CODE DISPLAYED HERE ]

Or run: warp receive http://192.168.1.15:45667/8f9a2b
Waiting for receiver...

```

### Command: `warp receive`

**Usage:** `warp receive [flags] <url>`

**Flags:**

* `--output, -o`: Specify output filename/directory (default: uses remote filename).
* `--force, -f`: Overwrite existing files without asking.

**Example Output:**

```text
$ warp receive http://192.168.1.15:45667/8f9a2b
> Connecting to 192.168.1.15...
> Verifying session... OK
> Receiving: vacation_photos.zip (Streaming)

[=====================>    ] 82% (15MB/s)

```

---

## 3. Detailed Implementation Phases & Testing Strategy

### Phase 1: Foundation (Network & Crypto)

* **Logic:**
* `network/ip.go`: Iterate over `net.Interfaces()`. Filter loopback/IPv6. Prioritize private ranges (`192.168.x.x`, `10.x.x.x`).
* `crypto/token.go`: Use `crypto/rand` to generate a 32-byte hex string.


* **Tests:**
* `ip_test.go`: Mock `net.Interface` data structures. Assert that the function correctly identifies "192.168.1.5" as valid and "127.0.0.1" as invalid.
* `token_test.go`: Generate 1,000 tokens. Assert strict uniqueness and correct length.



### Phase 2: The Sender (Server)

* **Logic:**
* `server/http.go`: Setup `http.Server`. Route `/d/{token}`.
* **Middleware:** Check token in URL. If invalid -> 404/403.
* **File Serving:** Use `http.ServeFile`.
* **Zip Serving:** If `os.Stat().IsDir()`, set header `Content-Type: application/zip`, create `zip.NewWriter(w)`, `filepath.Walk`, and copy file contents to the writer.


* **Tests:**
* `server_test.go`:
* **Scenario A:** Request file with **Valid** token. -> Assert 200 OK & Content-Length.
* **Scenario B:** Request file with **Invalid** token. -> Assert 403 Forbidden.
* **Scenario C:** Request **Directory**. -> Assert Content-Type is `application/zip` and Magic Bytes match PKZIP signature.





### Phase 3: The Receiver (Client)

* **Logic:**
* `client/receiver.go`: Parse URL. `http.Get()`. Check status code.
* Extract filename from `Content-Disposition` header.
* Create file on disk. `io.Copy(file, response.Body)`.


* **Tests:**
* `receiver_test.go`:
* Start a Mock HTTP Server (`httptest.NewServer`).
* Run Client against mock URL.
* Assert file is created on disk.
* Assert file hash matches mock data.





### Phase 4: UI Feedback (Progress & QR)

* **Logic:**
* `ui/progress.go`: Create struct `ProgressReader { R io.Reader, Total int64, Current int64 }`. Override `Read()` method to update `Current` and print `\r` (carriage return) with formatted string.
* `ui/qr.go`: Wrap `skip2/go-qrcode` generation.


* **Tests:**
* `ui_test.go`:
* Test `ProgressReader` with a `bytes.Buffer`. Ensure the "percentage" calculation is accurate at 0%, 50%, and 100%.





### Phase 5: Integration (E2E)

* **Logic:** `cmd/warp/main.go` logic to stitch it all together using `flag.Parse()`.
* **Tests (`test/e2e_test.go`):**
* **The "Golden Path":**
1. Create `temp_src/bigfile.bin` (100MB).
2. Start Sender in a goroutine on Port 0 (random).
3. Parse Sender output to get URL.
4. Run Receiver in main thread pointing to URL.
5. Wait for finish.
6. Compare `md5(temp_src/bigfile.bin)` vs `md5(temp_dest/bigfile.bin)`.





---

## 4. Dependencies

### Go Modules (`go.mod`)

```go
module github.com/yourname/warp-drop

go 1.21

require (
    github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
)

```

---

## 5. Build & Distribution Plan

1. **Development Run:**
```bash
go run cmd/warp/main.go send test_file.txt

```


2. **Compilation (Release):**
```bash
# Linux
CGO_ENABLED=0 go build -ldflags="-s -w" -o dist/warp-linux cmd/warp/main.go
# Mac (Intel/M1)
CGO_ENABLED=0 GOOS=darwin go build -o dist/warp-mac cmd/warp/main.go
# Windows
CGO_ENABLED=0 GOOS=windows go build -o dist/warp.exe cmd/warp/main.go

```


*Note: `CGO_ENABLED=0` ensures a static binary with no system library dependencies.*