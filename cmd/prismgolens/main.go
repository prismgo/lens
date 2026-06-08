// prismgolens 是 PrismGo Lens 的正式可安装 CLI 入口。
package main

import (
	"io"
	"os"

	"github.com/prismgo/lens/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args, os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	return cli.Run(args, os.Stdin, stdout, stderr)
}
