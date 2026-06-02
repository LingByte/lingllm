package main

import (
	"flag"
	"fmt"

	"github.com/LingByte/lingllm/version"
)

func main() {
	versionFlag := flag.Bool("version", false, "Show version information")
	shortVersion := flag.Bool("v", false, "Show version (short)")
	flag.Parse()

	if *versionFlag || *shortVersion {
		fmt.Println(version.GetVersionInfo())
		return
	}

	// Default: show help
	fmt.Println("LingLLM - A universal LLM application library")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  lingllm [options]")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -version    Show version information")
	fmt.Println("  -v          Show version (short)")
	fmt.Println()
	fmt.Println("Version:", version.GetVersion())
}
