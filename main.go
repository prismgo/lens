// prismgo-lens 是 PrismGo 的开发环境辅助 CLI，不应被生产应用 import。
package main

import (
	"io"
	"os"

	"github.com/prismgo/lens/internal/cli"
)

func main() {
	os.Exit(run(os.Args, os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	return cli.Run(args, stdin, stdout, stderr)
}
