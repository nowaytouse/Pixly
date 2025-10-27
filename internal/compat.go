package internal

import (
	"os"
	"strings"

	"golang.org/x/term"
)

// TerminalInfo contains terminal information
type TerminalInfo struct {
	Width  int
	Height int
}

// GetTerminalInfo retrieves terminal information - simple and reliable
func GetTerminalInfo() *TerminalInfo {
	info := &TerminalInfo{
		Width:  80, // Default width
		Height: 24, // Default height
	}

	// Try to get actual terminal size if available
	if term.IsTerminal(int(os.Stdout.Fd())) {
		width, height, err := term.GetSize(int(os.Stdout.Fd()))
		if err == nil && width > 0 && height > 0 {
			info.Width = width
			info.Height = height
		}
	}

	return info
}

// SupportsColor checks if colors should be used - simple rule
func SupportsColor() bool {
	return os.Getenv("NO_COLOR") == ""
}

// SupportsAnimation checks if animations should be used - simple rule
func SupportsAnimation() bool {
	return os.Getenv("CI") == "" && term.IsTerminal(int(os.Stdout.Fd()))
}

// SupportsUnicode checks if unicode should be used - simple rule
func SupportsUnicode() bool {
	lang := os.Getenv("LANG")
	return lang == "" || strings.Contains(strings.ToLower(lang), "utf-8")
}