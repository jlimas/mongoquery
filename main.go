package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		os.Exit(1)
	}
}

func run(args []string) (err error) {
	defer func() {
		if errors.Is(err, flag.ErrHelp) {
			err = nil
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(args) == 0 {
		return cmdHome(ctx)
	}
	switch args[0] {
	case "version", "--version":
		if len(args) > 1 {
			return unexpectedArgs(args[1:])
		}
		return printVersion(os.Stdout)
	case "home":
		if len(args) > 1 {
			return unexpectedArgs(args[1:])
		}
		return cmdHome(ctx)
	case "list":
		if len(args) > 1 {
			return unexpectedArgs(args[1:])
		}
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
		return fmt.Errorf("unknown subcommand: %s", args[0])
	}
}

func unexpectedArgs(args []string) error {
	err := fmt.Errorf("unexpected positional arguments: %v (flags must precede positional arguments)", args)
	printError(err.Error(), "")
	return err
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return unexpectedArgs(fs.Args())
	}
	return nil
}
