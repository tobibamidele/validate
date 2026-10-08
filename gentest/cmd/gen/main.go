// Command gen regenerates gentest/validators_gen.go.
package main

import (
	"fmt"
	"os"

	"github.com/tobibamidele/validate/gen"
	"github.com/tobibamidele/validate/gentest"
)

func main() {
	src, err := gen.Generate(gentest.Targets())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile("gentest/validators_gen.go", src, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
