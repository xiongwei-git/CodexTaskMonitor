package main

import (
	"os"

	"github.com/xiongwei-git/agent-task-monitor/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
