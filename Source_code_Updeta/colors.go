package main

import (
	"github.com/fatih/color"
	"sync"
)

// Color functions for consistent styling across the application
var (
	bold   = color.New(color.Bold).SprintFunc()
	cyan   = color.New(color.FgHiCyan).SprintFunc()
	green  = color.New(color.FgHiGreen).SprintFunc()
	yellow = color.New(color.FgHiYellow).SprintFunc()
	red    = color.New(color.FgHiRed).SprintFunc()
	violet = color.New(color.FgHiMagenta).SprintFunc()
	subtle = color.New(color.FgHiBlack).SprintFunc()
	
	consoleMutex = &sync.Mutex{}
)