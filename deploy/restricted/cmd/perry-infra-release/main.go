package main

import (
	"fmt"
	"os"

	"github.com/PerrySmithMoss/perry-infra/deploy/restricted/internal/release"
)

func main() {
	if err := release.Run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "Infrastructure release rejected or failed. Verify the administrator-installed snapshot and service health.")
		os.Exit(1)
	}
}
