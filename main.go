package main

import (
	"context"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		os.Exit(1)
	}
}

func run(args []string) error {
	ctx := context.Background()
	if len(args) == 0 {
		return cmdHome(ctx)
	}
	switch args[0] {
	case "home":
		return cmdHome(ctx)
	case "list":
		return cmdList(ctx)
	case "collections":
		return cmdCollections(ctx, args[1:])
	case "find":
		return cmdFind(ctx, args[1:])
	case "aggregate":
		return cmdAggregate(ctx, args[1:])
	case "count":
		return cmdCount(ctx, args[1:])
	case "distinct":
		return cmdDistinct(ctx, args[1:])
	case "-h", "--help", "help":
		return cmdHome(ctx)
	default:
		printError("unknown subcommand: "+args[0], "Run `mongoquery` to see available commands")
		os.Exit(2)
		return nil
	}
}
