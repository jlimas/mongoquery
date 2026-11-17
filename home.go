package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func cmdHome(_ context.Context) error {
	o := newOut()
	o.line("bin: %s", binPath())
	o.line("description: Read-only MongoDB query CLI for configured environments and services")
	o.line("commands: find, aggregate, count, distinct, collections, list, version")
	o.line("usage: mongoquery <command> --help")

	cfg, err := loadConfig()
	if err != nil {
		o.line("config: not loaded (%s)", err)
		printHelp(
			fmt.Sprintf("Copy config.example.yaml to %s and edit", configPath()),
			"Run `mongoquery list` once configured",
		)
		return nil
	}
	o.line("config: %s", cfg.path)

	envs := cfg.sortedEnvs()
	o.line("environments[%d]{name,services}:", len(envs))
	for _, e := range envs {
		svcs := sortedServices(cfg.Environments[e])
		o.line("  %s,%q", e, strings.Join(svcs, ","))
	}
	printHelp(
		"Run `mongoquery find --env <env> --service <svc> --collection <c> --query '{}'` to query",
		"Run `mongoquery aggregate --env <env> --service <svc> --collection <c> --pipeline '[]'` for pipelines",
		"Run `mongoquery collections --env <env> --service <svc>` to list collections",
		"Run `mongoquery <subcommand> --help` for flag reference",
	)
	return nil
}

func cmdList(_ context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		printError(err.Error(), fmt.Sprintf("Create %s using config.example.yaml as a template", configPath()))
		return err
	}
	envs := cfg.sortedEnvs()
	o := newOut()
	o.line("targets[%d]{env,service,database}:", countTargets(cfg))
	for _, e := range envs {
		for _, s := range sortedServices(cfg.Environments[e]) {
			svc := cfg.Environments[e].Services[s]
			db := svc.Database
			if db == "" {
				db = parseDBFromURI(svc.URI)
			}
			fmt.Fprintf(os.Stdout, "  %s,%s,%s\n", e, s, db)
		}
	}
	return nil
}

func countTargets(c *Config) int {
	n := 0
	for _, e := range c.Environments {
		n += len(e.Services)
	}
	return n
}

// binPath returns the executable path with $HOME collapsed to ~.
func binPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "mongoquery"
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(exe, home) {
		return "~" + exe[len(home):]
	}
	return exe
}
