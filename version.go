package main

import (
	"fmt"
	"io"
)

// version is set by the build's -X linker flag. Plain go builds report dev.
var version = "dev"

func printVersion(w io.Writer) error {
	_, err := fmt.Fprintf(w, "mongoquery %s\n", version)
	return err
}
