package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type ServiceConfig struct {
	URI      string `yaml:"uri"`
	Database string `yaml:"database,omitempty"`
}

type EnvironmentConfig struct {
	Services map[string]ServiceConfig `yaml:"services"`
}

type Config struct {
	Environments map[string]EnvironmentConfig `yaml:"environments"`
	path         string
}

func configPath() string {
	if p := os.Getenv("MONGOQUERY_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".mongoquery.yaml"
	}
	return filepath.Join(home, ".mongoquery.yaml")
}

func loadConfig() (*Config, error) {
	p := configPath()
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config not found at %s — copy config.example.yaml there", p)
		}
		return nil, fmt.Errorf("cannot read config %s: %w", p, err)
	}
	var c Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&c); err != nil {
		return nil, fmt.Errorf("invalid YAML in %s: %w", p, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("config %s must contain exactly one YAML document", p)
	}
	if len(c.Environments) == 0 {
		return nil, fmt.Errorf("config %s has no environments", p)
	}
	c.path = p
	return &c, nil
}

func (c *Config) resolve(env, service string) (ServiceConfig, error) {
	e, ok := c.Environments[env]
	if !ok {
		return ServiceConfig{}, fmt.Errorf("unknown env %q (configured: %s)", env, joinKeys(c.Environments))
	}
	s, ok := e.Services[service]
	if !ok {
		return ServiceConfig{}, fmt.Errorf("unknown service %q in env %q (configured: %s)", service, env, joinKeys(e.Services))
	}
	if s.URI == "" {
		return ServiceConfig{}, fmt.Errorf("service %q in env %q has empty uri", service, env)
	}
	return s, nil
}

func (c *Config) sortedEnvs() []string {
	out := make([]string, 0, len(c.Environments))
	for k := range c.Environments {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedServices(e EnvironmentConfig) []string {
	out := make([]string, 0, len(e.Services))
	for k := range e.Services {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func joinKeys[V any](m map[string]V) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	s := ""
	for i, k := range keys {
		if i > 0 {
			s += ","
		}
		s += k
	}
	if s == "" {
		return "<none>"
	}
	return s
}
