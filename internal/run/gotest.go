package run

import (
	"os"
	"strings"
)

// underGoTest reports whether this process is a `go test` binary.
func underGoTest() bool {
	return strings.HasSuffix(os.Args[0], ".test") || strings.Contains(os.Args[0], ".test.")
}
