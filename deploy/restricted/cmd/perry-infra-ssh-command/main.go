package main

import (
	"fmt"
	"os"
	"syscall"

	"github.com/PerrySmithMoss/perry-infra/deploy/restricted/internal/release"
)

func main() {
	args := release.SSHArgs(os.Getenv("SSH_ORIGINAL_COMMAND"))
	if len(os.Args) != 1 || args == nil {
		fmt.Fprintln(os.Stderr, "Only the reviewed infrastructure deployment is allowed.")
		os.Exit(1)
	}

	const sudo = "/usr/bin/sudo"
	if err := syscall.Exec(sudo, append([]string{sudo, "-n", "/usr/local/libexec/perry-infra-release"}, args...), nil); err != nil {
		fmt.Fprintln(os.Stderr, "Could not start the restricted infrastructure deployment.")
		os.Exit(1)
	}
}
