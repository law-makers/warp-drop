package server

import (
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zulfikawr/warp-drop/internal/network"
	"github.com/zulfikawr/warp-drop/internal/protocol"
)

type Server struct {
	InterfaceName string
	Token         string
	SrcPath       string
	// Host mode (reverse drop)
	HostMode   bool
	UploadDir  string
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
	if s.HostMode {
		mux.HandleFunc(protocol.UploadPathPrefix, s.handleUpload)
	} else {
		mux.HandleFunc(protocol.PathPrefix, s.handleDownload)
	}

	s.httpServer = &http.Server{
		ReadTimeout:       protocol.ReadTimeout,
		ReadHeaderTimeout: 30 * time.Second,
		WriteTimeout:      protocol.WriteTimeout,
		IdleTimeout:       protocol.IdleTimeout,
		MaxHeaderBytes:    1 << 20, // 1MB
		Handler:           mux,
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

	if s.HostMode {
		return fmt.Sprintf("http://%s:%d%s%s", ip.String(), s.Port, protocol.UploadPathPrefix, s.Token), nil
	}
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
	
	// Support resumable downloads via Range headers
	f, err := os.Open(s.SrcPath)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close()
	
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" && strings.HasPrefix(rangeHeader, "bytes=") {
		// Parse Range: bytes=start-end
		rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
		var start int64
		if _, err := fmt.Sscanf(rangeSpec, "%d-", &start); err == nil && start > 0 {
			if _, err := f.Seek(start, 0); err == nil {
				w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, fi.Size()-1, fi.Size()))
				w.Header().Set("Content-Length", fmt.Sprintf("%d", fi.Size()-start))
				w.WriteHeader(http.StatusPartialContent)
				io.Copy(w, f)
				log.Printf("Resumed download from byte %d for %s", start, filepath.Base(s.SrcPath))
				return
			}
		}
	}
	
	// Normal full file download
	http.ServeFile(w, r, s.SrcPath)
}

// handleUpload serves a simple HTML form on GET and accepts multipart file uploads on POST.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Expect /u/{token}
	p := strings.TrimPrefix(r.URL.Path, protocol.UploadPathPrefix)
	if p != s.Token {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	if r.Method == http.MethodGet {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Warp Drop</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{
  font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Oxygen,Ubuntu,Cantarell,sans-serif;
  background:#0a0a0a;
  color:#e5e5e5;
  min-height:100vh;
  display:flex;
  align-items:center;
  justify-content:center;
  padding:1.5rem;
  line-height:1.6
}
.container{
  max-width:480px;
  width:100%
}
.header{
  text-align:center;
  margin-bottom:2rem
}
.title{
  font-size:1.75rem;
  font-weight:600;
  letter-spacing:-0.025em;
  margin-bottom:0.5rem
}
.subtitle{
  color:#a3a3a3;
  font-size:0.925rem
}
.card{
  background:#171717;
  border:1px solid #262626;
  border-radius:16px;
  padding:2rem;
  transition:border-color 0.2s ease
}
.upload-zone{
  border:2px dashed #404040;
  border-radius:12px;
  padding:2.5rem 1.5rem;
  text-align:center;
  transition:all 0.2s ease;
  cursor:pointer;
  background:#0a0a0a;
  margin-bottom:1.5rem
}
.upload-zone:hover{
  border-color:#737373;
  background:#171717
}
.upload-zone.dragover{
  border-color:#e5e5e5;
  background:#262626
}
.upload-icon{
  width:48px;
  height:48px;
  margin:0 auto 1rem;
  opacity:0.6
}
.upload-text{
  font-size:0.95rem;
  color:#d4d4d4;
  margin-bottom:0.5rem
}
.upload-hint{
  font-size:0.825rem;
  color:#737373
}
input[type=file]{
  display:none
}
.file-list{
  margin-bottom:1.5rem;
  max-height:200px;
  overflow-y:auto
}
.file-item{
  display:flex;
  align-items:center;
  justify-content:space-between;
  padding:0.75rem;
  background:#0a0a0a;
  border:1px solid #262626;
  border-radius:8px;
  margin-bottom:0.5rem;
  font-size:0.875rem
}
.file-name{
  color:#e5e5e5;
  flex:1;
  overflow:hidden;
  text-overflow:ellipsis;
  white-space:nowrap;
  margin-right:1rem
}
.file-size{
  color:#737373;
  font-size:0.8rem;
  margin-right:0.75rem
}
.remove-btn{
  background:transparent;
  border:none;
  color:#a3a3a3;
  cursor:pointer;
  padding:0.25rem;
  font-size:1.25rem;
  line-height:1;
  transition:color 0.2s ease
}
.remove-btn:hover{
  color:#e5e5e5
}
.btn{
  width:100%;
  padding:0.875rem;
  background:#e5e5e5;
  color:#0a0a0a;
  border:none;
  border-radius:10px;
  font-size:0.95rem;
  font-weight:500;
  cursor:pointer;
  transition:all 0.2s ease
}
.btn:hover{
  background:#fff
}
.btn:disabled{
  background:#262626;
  color:#737373;
  cursor:not-allowed
}
.footer{
  text-align:center;
  margin-top:1.5rem;
  font-size:0.8rem;
  color:#525252
}
</style>
</head>
<body>
<div class="container">
  <div class="header">
    <h1 class="title">Warp Drop</h1>
    <p class="subtitle">Transfer files securely</p>
  </div>
  
  <div class="card">
    <form method="POST" enctype="multipart/form-data" id="uploadForm">
      <div class="upload-zone" id="dropZone">
        <svg class="upload-icon" fill="none" stroke="currentColor" viewBox="0 0 24 24">
          <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M7 16a4 4 0 01-.88-7.903A5 5 0 1115.9 6L16 6a5 5 0 011 9.9M15 13l-3-3m0 0l-3 3m3-3v12"/>
        </svg>
        <div class="upload-text">Click to select files</div>
        <div class="upload-hint">or drag and drop here</div>
      </div>
      <input type="file" name="file" id="fileInput" multiple required>
      
      <div class="file-list" id="fileList"></div>
      
      <button type="submit" class="btn" id="submitBtn" disabled>Upload Files</button>
    </form>
  </div>
  
  <div class="footer">Token-protected • Not cached</div>
</div>

<script>
const dropZone=document.getElementById('dropZone');
const fileInput=document.getElementById('fileInput');
const fileList=document.getElementById('fileList');
const submitBtn=document.getElementById('submitBtn');
let selectedFiles=[];

dropZone.addEventListener('click',()=>fileInput.click());

fileInput.addEventListener('change',e=>{
  handleFiles(e.target.files);
});

['dragenter','dragover','dragleave','drop'].forEach(evt=>{
  dropZone.addEventListener(evt,e=>{
    e.preventDefault();
    e.stopPropagation();
  });
});

['dragenter','dragover'].forEach(evt=>{
  dropZone.addEventListener(evt,()=>dropZone.classList.add('dragover'));
});

['dragleave','drop'].forEach(evt=>{
  dropZone.addEventListener(evt,()=>dropZone.classList.remove('dragover'));
});

dropZone.addEventListener('drop',e=>{
  const files=e.dataTransfer.files;
  handleFiles(files);
});

function handleFiles(files){
  selectedFiles=Array.from(files);
  updateFileList();
  submitBtn.disabled=selectedFiles.length===0;
}

function updateFileList(){
  if(selectedFiles.length===0){
    fileList.innerHTML='';
    return;
  }
  const items=[];
  for(let i=0;i<selectedFiles.length;i++){
    const f=selectedFiles[i];
    items.push(
      '<div class="file-item">'
      + '<span class="file-name">' + escapeHtml(f.name) + '</span>'
      + '<span class="file-size">' + formatSize(f.size) + '</span>'
      + '<button type="button" class="remove-btn" onclick="removeFile(' + i + ')">×</button>'
      + '</div>'
      + '<div style="height:8px;background:#1f2937;border-radius:8px;margin-top:.5rem;overflow:hidden">'
      + '  <div id="bar-' + i + '" style="height:100%;background:#22c55e;width:0%"></div>'
      + '</div>'
      + '<div id="speed-' + i + '" style="color:#9ca3af;font-size:.8rem;margin-top:.35rem">0.0 Mbps</div>'
    );
  }
  fileList.innerHTML=items.join('');
}

function removeFile(idx){
  selectedFiles.splice(idx,1);
  const dt=new DataTransfer();
  selectedFiles.forEach(file=>dt.items.add(file));
  fileInput.files=dt.files;
  updateFileList();
  submitBtn.disabled=selectedFiles.length===0;
}

function formatSize(bytes){
  if(bytes<1024)return bytes+' B';
  if(bytes<1048576)return(bytes/1024).toFixed(1)+' KB';
  return(bytes/1048576).toFixed(1)+' MB';
}

function escapeHtml(text){
  const div=document.createElement('div');
  div.textContent=text;
  return div.innerHTML;
}
// Handle custom upload with progress per file
const form=document.getElementById('uploadForm');
form.addEventListener('submit', async (e)=>{
  e.preventDefault();
  submitBtn.disabled=true;
  for(let i=0;i<selectedFiles.length;i++){
    try {
      await uploadFile(selectedFiles[i], i);
    } catch(err) {
      const sp=document.getElementById('speed-' + i);
      if(sp){ sp.textContent='Failed'; }
    }
  }
  submitBtn.textContent='Done';
});

function uploadFile(file, idx){
  return new Promise((resolve, reject)=>{
    const xhr=new XMLHttpRequest();
    xhr.open('POST', window.location.pathname);
    const fd=new FormData();
    fd.append('file', file, file.name);
    const start=Date.now();
    xhr.upload.onprogress=(e)=>{
      if(e.lengthComputable){
        const pct=Math.min(100, (e.loaded / file.size) * 100);
        const bar=document.getElementById('bar-' + idx);
        if(bar){ bar.style.width=pct.toFixed(1) + '%'; }
        const elapsed=(Date.now() - start)/1000;
        let mbps=0;
        if(elapsed > 0){ mbps=(e.loaded * 8) / (elapsed * 1000000); }
        const sp=document.getElementById('speed-' + idx);
        if(sp){ sp.textContent=mbps.toFixed(1) + ' Mbps'; }
      }
    };
    xhr.onreadystatechange=function(){
      if(xhr.readyState===XMLHttpRequest.DONE){
        if(xhr.status>=200 && xhr.status<300){ resolve(); }
        else { reject(new Error('Upload failed: ' + xhr.status)); }
      }
    };
    xhr.send(fd);
  });
}</script>
</body>
</html>`)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestStart := time.Now()

	// Basic upload security and limits: limit request size if Content-Length present
	// and prevent caching of responses.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")

	// Ensure upload dir exists
	dest := s.UploadDir
	if dest == "" {
		dest = "."
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// Set max upload size to 10GB for large file support
	r.Body = http.MaxBytesReader(w, r.Body, 10<<30) // 10GB limit

	// Parse multipart form; keep 256MB in memory, rest spills to temp files
	if err := r.ParseMultipartForm(256 << 20); err != nil {
		log.Printf("Failed to parse multipart form: %v", err)
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		http.Error(w, "no file provided", http.StatusBadRequest)
		return
	}

	type savedInfo struct{ Name string; Size int64 }
	var saved []savedInfo

	for _, fh := range files {
		// Sanitize filename
		name := filepath.Base(fh.Filename)
		src, err := fh.Open()
		if err != nil {
			http.Error(w, "upload error", http.StatusInternalServerError)
			return
		}
		defer src.Close()

		outPath := filepath.Join(dest, name)
		out, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			log.Printf("Failed to create file %s: %v", name, err)
			http.Error(w, "write error", http.StatusInternalServerError)
			return
		}
		// Use larger buffer for better performance on large files
		buf := make([]byte, 1<<20) // 1MB buffer
		n, err := io.CopyBuffer(out, src, buf)
		cerr := out.Close()
		if err != nil || cerr != nil {
			log.Printf("Failed to write file %s: write_err=%v, close_err=%v", name, err, cerr)
			http.Error(w, "write error", http.StatusInternalServerError)
			return
		}
		duration := time.Since(requestStart).Seconds()
		log.Printf("%s, %s received in %.2fs", name, formatBytes(n), duration)
		saved = append(saved, savedInfo{Name: name, Size: n})
		requestStart = time.Now() // Reset for next file in batch
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	io.WriteString(w, `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Upload Complete</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{
  font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Oxygen,Ubuntu,Cantarell,sans-serif;
  background:#0a0a0a;
  color:#e5e5e5;
  min-height:100vh;
  display:flex;
  align-items:center;
  justify-content:center;
  padding:1.5rem;
  line-height:1.6
}
.container{
  max-width:480px;
  width:100%
}
.card{
  background:#171717;
  border:1px solid #262626;
  border-radius:16px;
  padding:2rem;
  text-align:center
}
.success-icon{
  width:64px;
  height:64px;
  margin:0 auto 1.5rem;
  color:#a3a3a3
}
.title{
  font-size:1.5rem;
  font-weight:600;
  margin-bottom:0.5rem
}
.subtitle{
  color:#a3a3a3;
  font-size:0.925rem;
  margin-bottom:2rem
}
.file-list{
  text-align:left;
  margin-bottom:2rem
}
.file-item{
  display:flex;
  align-items:center;
  justify-content:space-between;
  padding:0.875rem;
  background:#0a0a0a;
  border:1px solid #262626;
  border-radius:8px;
  margin-bottom:0.5rem
}
.file-name{
  color:#e5e5e5;
  font-size:0.9rem;
  flex:1;
  overflow:hidden;
  text-overflow:ellipsis;
  white-space:nowrap;
  margin-right:1rem
}
.file-size{
  color:#737373;
  font-size:0.825rem
}
.btn{
  width:100%;
  padding:0.875rem;
  background:#e5e5e5;
  color:#0a0a0a;
  border:none;
  border-radius:10px;
  font-size:0.95rem;
  font-weight:500;
  cursor:pointer;
  text-decoration:none;
  display:inline-block;
  transition:all 0.2s ease
}
.btn:hover{
  background:#fff
}
</style>
</head>
<body>
<div class="container">
  <div class="card">
    <svg class="success-icon" fill="none" stroke="currentColor" viewBox="0 0 24 24">
      <path stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5" d="M9 12l2 2 4-4m6 2a9 9 0 11-18 0 9 9 0 0118 0z"/>
    </svg>
    <h1 class="title">Upload Complete</h1>
    <p class="subtitle">`)
	io.WriteString(w, fmt.Sprintf("%d file(s) transferred successfully", len(saved)))
	io.WriteString(w, `</p>
    <div class="file-list">`)
	for _, s := range saved {
		sizeStr := formatBytes(s.Size)
		io.WriteString(w, fmt.Sprintf(`<div class="file-item">
        <span class="file-name">%s</span>
        <span class="file-size">%s</span>
      </div>`, s.Name, sizeStr))
	}
	io.WriteString(w, `</div>
    <a href="" class="btn">Upload More Files</a>
  </div>
</div>
</body>
</html>`)
}

// formatBytes formats bytes into human-readable string
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// Shutdown stops the server.
func (s *Server) Shutdown() error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Close()
}