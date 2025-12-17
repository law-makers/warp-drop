package protocol

import "time"

const (
	PathPrefix = "/d/"
	UploadPathPrefix = "/u/"
)

var (
	ReadTimeout  = 30 * time.Second
	WriteTimeout = 30 * time.Second
	IdleTimeout  = 60 * time.Second
)
