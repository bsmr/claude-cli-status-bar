package cli

import (
	"fmt"
	"io"

	"go.muehmer.eu/claude-cli-status-bar/internal/pkg/config"
)

// defaultProxyCommand and defaultProxyArgs are used when `ccsb mode proxy` is
// invoked without an explicit command. They mirror what `install` picks up
// from a stock Claude Code settings.json that points at ccstatusline.
const defaultProxyCommand = "npx"

var defaultProxyArgs = []string{"-y", "ccstatusline@latest"}

// runMode handles the `ccsb mode` subcommand.
//
// With no args, it prints the current mode ("native" or "proxy") followed by
// a newline. With "native" as the argument, it zeros cfg.Proxy so the omitzero
// JSON tag drops the proxy key from config.json. With "proxy" it writes the
// proxy command; if no command is supplied the npx/ccstatusline default is used.
func runMode(p Paths, args []string, stdout io.Writer) error {
	cfg, err := config.Load(p.Config)
	if err != nil {
		return err
	}

	if len(args) == 0 {
		fmt.Fprintln(stdout, currentMode(cfg))
		return nil
	}

	switch args[0] {
	case "native":
		if len(args) > 1 {
			return fmt.Errorf("ccsb: native mode takes no arguments, got %d", len(args)-1)
		}
		cfg.Proxy = config.Proxy{}
		return config.Save(p.Config, cfg)
	case "proxy":
		cfg.Proxy = proxyFromArgs(args[1:])
		return config.Save(p.Config, cfg)
	}

	return fmt.Errorf("ccsb: invalid mode %q (valid: native, proxy)", args[0])
}

// proxyFromArgs builds a Proxy from the tokens after `proxy`. Empty input
// produces the npx/ccstatusline default; otherwise the first token becomes
// the command and the rest the arguments. Args are copied so the saved
// config never aliases the caller's argv.
func proxyFromArgs(tokens []string) config.Proxy {
	if len(tokens) == 0 {
		return config.Proxy{
			Command: defaultProxyCommand,
			Args:    append([]string(nil), defaultProxyArgs...),
		}
	}
	if len(tokens) == 1 {
		return config.Proxy{Command: tokens[0]}
	}
	return config.Proxy{
		Command: tokens[0],
		Args:    append([]string(nil), tokens[1:]...),
	}
}

// currentMode reports "native" or "proxy" based on whether a proxy command
// is configured. This is the single source of truth for the mode label.
func currentMode(cfg config.Config) string {
	if cfg.Proxy.Command == "" {
		return "native"
	}
	return "proxy"
}
