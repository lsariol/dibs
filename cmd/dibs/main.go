// Command dibs is the Dibs card-table server. One binary runs the server or
// one small operator command, picked by the first argument.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lsariol/dibs/internal/config"
	"github.com/lsariol/dibs/internal/server"
)

// command is one entry of the command table; help comes from it.
type command struct {
	name    string
	summary string
	usage   string
	example string
	run     func(args []string) int
}

var commands []command

func init() {
	commands = []command{
		{"serve", "Run the server (what the compose file starts)", "dibs serve", "dibs serve", runServe},
		{"health", "Exit 0 if the server answers its health check", "dibs health", "docker exec dibs /dibs health", runHealth},
		{"version", "Print the version", "dibs version", "dibs version", runVersion},
		{"help", "Show commands, or one command's usage", "dibs help [command]", "dibs help serve", runHelp},
	}
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		os.Exit(runHelp(nil))
	}
	if args[0] == "--version" {
		os.Exit(runVersion(nil))
	}
	for _, c := range commands {
		if c.name == args[0] {
			os.Exit(c.run(args[1:]))
		}
	}
	fmt.Fprintf(os.Stderr, "! Unknown command %q. Run \"dibs help\" to list the commands.\n", args[0])
	os.Exit(1)
}

func runHelp(args []string) int {
	if len(args) > 0 {
		for _, c := range commands {
			if c.name == args[0] {
				fmt.Printf("%s\n\nUsage:\n  %s\n\nExample:\n  %s\n", c.summary, c.usage, c.example)
				return 0
			}
		}
		fmt.Fprintf(os.Stderr, "! Unknown command %q.\nUsage: dibs help [command]\n", args[0])
		return 1
	}
	printUsage(os.Stdout)
	return 0
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "Usage: dibs <command>")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, c := range commands {
		fmt.Fprintf(w, "  %-9s %s\n", c.name, c.summary)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Settings come from the environment: APP_ENV, DIBS_ADDR, DIBS_PUBLIC_URL,")
	fmt.Fprintln(w, "DIBS_CLIENT_IP_HEADER, DIBS_VERSION. See README.md.")
}

func runVersion(args []string) int {
	fmt.Println(loadConfig().Version)
	return 0
}

// runServe runs the server until SIGINT or SIGTERM (docker stop).
func runServe(args []string) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "! serve takes no arguments.\nUsage: dibs serve")
		return 1
	}
	cfg := loadConfig()
	log := newLogger(cfg.Env)
	log.Info("Dibs starting", "version", cfg.Version, "env", cfg.Env)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	srv, err := server.New(cfg, log)
	if err != nil {
		log.Error(err.Error())
		return 1
	}
	if err := srv.Run(ctx); err != nil {
		log.Error(err.Error())
		return 1
	}
	log.Info("Dibs stopped")
	return 0
}

// runHealth asks the server inside this container; the compose healthcheck uses it.
func runHealth(args []string) int {
	cfg := loadConfig()
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(cfg.HealthURL())
	if err != nil {
		fmt.Fprintf(os.Stderr, "✗ Dibs isn't answering on %s: %v. Check \"docker logs dibs\".\n", cfg.HealthURL(), err)
		return 1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "✗ Dibs's health check answered %s. Check \"docker logs dibs\".\n", resp.Status)
		return 1
	}
	return 0
}

func loadConfig() config.Config {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, "✗", err)
		os.Exit(1)
	}
	return cfg
}

// newLogger logs text lines with UTC times to stderr; dev adds debug lines.
func newLogger(env string) *slog.Logger {
	level := slog.LevelInfo
	if strings.EqualFold(env, "dev") {
		level = slog.LevelDebug
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && len(groups) == 0 {
				a.Value = slog.TimeValue(a.Value.Time().UTC())
			}
			return a
		},
	})
	return slog.New(h)
}
