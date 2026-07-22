package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/xiongwei-git/agent-task-monitor/internal/api"
	"github.com/xiongwei-git/agent-task-monitor/internal/monitor"
	"github.com/xiongwei-git/agent-task-monitor/internal/probe"
	"github.com/xiongwei-git/agent-task-monitor/internal/projectprefs"
	"github.com/xiongwei-git/agent-task-monitor/internal/projectprocess"
	webui "github.com/xiongwei-git/agent-task-monitor/web"
)

const defaultStaleAfter = 10 * time.Minute

func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		writeUsage(stdout)
		return 0
	}

	switch args[0] {
	case "probe":
		return runProbe(args[1:], stdout, stderr)
	case "serve":
		return runServe(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown command %q\n\n", args[0])
		writeUsage(stderr)
		return 2
	}
}

func runServe(args []string, stdout, stderr io.Writer) int {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "serve failed: resolve user home")
		return 1
	}
	defaultCodexHome := os.Getenv("CODEX_HOME")
	if defaultCodexHome == "" {
		defaultCodexHome = filepath.Join(homeDir, ".codex")
	}

	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	addr := flags.String("addr", "127.0.0.1:4747", "loopback listen address")
	codexHome := flags.String("codex-home", defaultCodexHome, "Codex state directory")
	projectRoot := flags.String("project-root", filepath.Join(homeDir, "CodeX"), "durable project root")
	staleAfter := flags.Duration("stale-after", defaultStaleAfter, "inactivity threshold for suspected abnormal tasks")
	refreshEvery := flags.Duration("refresh", 2*time.Second, "snapshot refresh interval")
	preferencesPath := flags.String("project-preferences", "", "project preference file (defaults under ProjectNavigator)")
	flags.Usage = func() { writeServeUsage(stderr) }
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "serve does not accept positional arguments")
		return 2
	}
	if !validLoopbackAddress(*addr) {
		fmt.Fprintln(stderr, "addr must be a loopback address such as 127.0.0.1:4747")
		return 2
	}
	if *staleAfter <= 0 || *refreshEvery < 500*time.Millisecond {
		fmt.Fprintln(stderr, "stale-after must be positive and refresh must be at least 500ms")
		return 2
	}
	resolvedPreferencesPath := *preferencesPath
	if resolvedPreferencesPath == "" {
		resolvedPreferencesPath = filepath.Join(*projectRoot, "ProjectNavigator", "project-preferences.json")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	projectProcessObserver := projectprocess.NewObserver(projectprocess.Options{})
	preferenceStore := projectprefs.NewStore(projectprefs.Options{Path: resolvedPreferencesPath})
	store := monitor.NewStore(func(runContext context.Context) (probe.Report, error) {
		return probe.Run(runContext, probe.Options{
			StateDBPath:            filepath.Join(*codexHome, "state_5.sqlite"),
			ProjectRoot:            *projectRoot,
			ProjectPreferencesPath: resolvedPreferencesPath,
			StaleAfter:             *staleAfter,
			DetectProjectProcesses: projectProcessObserver.Detect,
		})
	})
	if _, err := store.Refresh(ctx); err != nil {
		fmt.Fprintln(stderr, "serve failed: Codex state is unavailable")
		return 1
	}

	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(stderr, "serve failed: listen address is unavailable")
		return 1
	}
	defer listener.Close()

	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
	server := &http.Server{
		Handler:           api.NewHandler(store, preferenceStore, webui.Assets),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
	go store.Start(ctx, *refreshEvery, func(error) {
		logger.Warn("snapshot refresh failed")
	})
	go func() {
		<-ctx.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()

	actualAddr := listener.Addr().String()
	fmt.Fprintf(stdout, "AgentTaskMonitor dashboard: http://%s\n", actualAddr)
	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(stderr, "serve failed: local server stopped unexpectedly")
		return 1
	}
	return 0
}

func runProbe(args []string, stdout, stderr io.Writer) int {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(stderr, "probe failed: resolve user home")
		return 1
	}

	defaultCodexHome := os.Getenv("CODEX_HOME")
	if defaultCodexHome == "" {
		defaultCodexHome = filepath.Join(homeDir, ".codex")
	}

	flags := flag.NewFlagSet("probe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	codexHome := flags.String("codex-home", defaultCodexHome, "Codex state directory")
	projectRoot := flags.String("project-root", filepath.Join(homeDir, "CodeX"), "durable project root")
	staleAfter := flags.Duration("stale-after", defaultStaleAfter, "inactivity threshold for suspected abnormal tasks")
	pretty := flags.Bool("pretty", true, "pretty-print JSON output")
	flags.Usage = func() { writeProbeUsage(stderr) }

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "probe does not accept positional arguments")
		return 2
	}
	if *staleAfter <= 0 {
		fmt.Fprintln(stderr, "stale-after must be positive")
		return 2
	}

	report, err := probe.Run(context.Background(), probe.Options{
		StateDBPath: filepath.Join(*codexHome, "state_5.sqlite"),
		ProjectRoot: *projectRoot,
		StaleAfter:  *staleAfter,
	})
	if err != nil {
		fmt.Fprintf(stderr, "probe failed: %v\n", err)
		return 1
	}

	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(true)
	if *pretty {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(report); err != nil {
		fmt.Fprintln(stderr, "probe failed: encode report")
		return 1
	}
	return 0
}

func writeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "AgentTaskMonitor local Codex dashboard")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Usage:")
	fmt.Fprintln(writer, "  agent-task-monitor serve [flags]")
	fmt.Fprintln(writer, "  agent-task-monitor probe [flags]")
	fmt.Fprintln(writer, "  agent-task-monitor help")
}

func writeServeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: agent-task-monitor serve [flags]")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Starts the dashboard on a loopback address; Codex state remains read-only.")
}

func writeProbeUsage(writer io.Writer) {
	fmt.Fprintln(writer, "Usage: agent-task-monitor probe [flags]")
	fmt.Fprintln(writer)
	fmt.Fprintln(writer, "Reads Codex metadata and lifecycle events without modifying Codex state.")
}

func validLoopbackAddress(value string) bool {
	host, port, err := net.SplitHostPort(value)
	if err != nil || port == "" {
		return false
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
