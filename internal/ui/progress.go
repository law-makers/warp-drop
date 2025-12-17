package ui

import (
	"fmt"
	"io"
)

type ProgressReader struct {
	R       io.Reader
	Total   int64
	Current int64
	Out     io.Writer
}

func (p *ProgressReader) Read(b []byte) (int, error) {
	n, err := p.R.Read(b)
	p.Current += int64(n)
	if p.Total > 0 && p.Out != nil {
		pct := float64(p.Current) / float64(p.Total) * 100.0
		fmt.Fprintf(p.Out, "\r[%-20s] %3.0f%%", bar(pct), pct)
	}
	return n, err
}

func bar(pct float64) string {
	filled := int(pct / 5)
	if filled < 0 { filled = 0 }
	if filled > 20 { filled = 20 }
	return repeat("=", filled) + repeat(" ", 20-filled)
}

func repeat(s string, n int) string {
	res := ""
	for i := 0; i < n; i++ { res += s }
	return res
}
