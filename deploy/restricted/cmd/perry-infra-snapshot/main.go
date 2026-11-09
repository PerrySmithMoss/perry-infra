package main

import (
	"fmt"
	"os"

	"github.com/PerrySmithMoss/perry-infra/deploy/restricted/internal/release"
)

func main() {
	var value string
	var err error
	switch {
	case len(os.Args) == 3 && os.Args[1] == "fingerprint":
		value, err = release.Fingerprint(os.Args[2])
	case len(os.Args) == 2 && os.Args[1] == "acme-env":
		value, err = release.ACMEEnv(os.Stdin)
	default:
		fmt.Fprintln(os.Stderr, "Usage: perry-infra-snapshot fingerprint ROOT | acme-env")
		os.Exit(1)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(value)
}
