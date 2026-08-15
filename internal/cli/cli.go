// Package cli implements the CaveLoop command line interface.
//
// Every subcommand shares the same global flags and the same exit code
// convention:
//
//	0  the command succeeded
//	1  the command could not run, for example a bad flag or an unreadable file
//	2  the command ran and the data was rejected, for example an invalid survey
//	   or a broken audit chain
//
// The CLI performs no network access of any kind and only ever touches the
// files it is pointed at.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"CaveLoop/internal/config"
	"CaveLoop/internal/report"
	"CaveLoop/internal/store"
)

// Version is the CaveLoop release identifier.
const Version = "1.0.0"

// Exit codes returned by Run.
const (
	ExitOK      = 0
	ExitError   = 1
	ExitRefused = 2
)

// commandHandler executes one subcommand.
type commandHandler func(*Environment, []string) int

// command describes a subcommand.
type command struct {
	name    string
	summary string
	handler commandHandler
}

// Environment carries the process level writers used by the CLI.
type Environment struct {
	Stdout io.Writer
	Stderr io.Writer
}

// commands lists every subcommand in help order.
func commands() []command {
	return []command{
		{"validate", "strictly decode a survey document and report every finding", runValidate},
		{"import", "append a survey document to the append only ledger", runImport},
		{"reduce", "reduce the ledger, lay out coordinates and write the snapshot", runReduce},
		{"adjust", "distribute loop closure error and write the adjusted snapshot", runAdjust},
		{"network", "describe the survey graph topology", runNetwork},
		{"loops", "list the independent loops and their closure errors", runLoops},
		{"blunders", "scan the survey for suspected gross errors", runBlunders},
		{"report", "print the full survey report", runReport},
		{"verify", "verify the store digests and the audit chain", runVerify},
	}
}

// Run dispatches a subcommand and returns the process exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	env := &Environment{Stdout: stdout, Stderr: stderr}
	if len(args) == 0 {
		usage(env.Stderr)
		return ExitError
	}
	name := args[0]
	switch name {
	case "-h", "--help", "help":
		usage(env.Stdout)
		return ExitOK
	case "-version", "--version", "version":
		fmt.Fprintf(env.Stdout, "CaveLoop %s\n", Version)
		return ExitOK
	}
	for _, candidate := range commands() {
		if candidate.name != name {
			continue
		}
		return candidate.handler(env, args[1:])
	}
	fmt.Fprintf(env.Stderr, "unknown subcommand %q\n\n", name)
	usage(env.Stderr)
	return ExitError
}

// usage prints the command overview.
func usage(out io.Writer) {
	fmt.Fprintf(out, "CaveLoop %s - offline cave survey reduction and traverse adjustment\n\n", Version)
	fmt.Fprintf(out, "usage: caveloop <subcommand> [flags]\n\nsubcommands:\n")
	for _, candidate := range commands() {
		fmt.Fprintf(out, "  %-9s %s\n", candidate.name, candidate.summary)
	}
	fmt.Fprintf(out, "\ncommon flags:\n")
	fmt.Fprintf(out, "  -config path   strict JSON configuration document\n")
	fmt.Fprintf(out, "  -data dir      store directory, overrides the configured dataDir\n")
	fmt.Fprintf(out, "  -format kind   output format, text or json\n")
	fmt.Fprintf(out, "  -out path      write the report to a file instead of standard output\n")
	fmt.Fprintf(out, "\nexit codes: 0 success, 1 command error, 2 data refused\n")
}

// commonFlags holds the flags shared by every subcommand.
type commonFlags struct {
	configPath string
	dataDir    string
	format     string
	outPath    string
}

// bind registers the common flags on a flag set.
func (c *commonFlags) bind(flags *flag.FlagSet) {
	flags.StringVar(&c.configPath, "config", "", "path to a strict JSON configuration document")
	flags.StringVar(&c.dataDir, "data", "", "store directory, overrides the configured dataDir")
	flags.StringVar(&c.format, "format", "", "output format: text or json")
	flags.StringVar(&c.outPath, "out", "", "write the report to this file instead of standard output")
}

// session bundles the resolved configuration, output sink and store handle.
type session struct {
	cfg      config.Config
	renderer *report.Renderer
	store    *store.Store
	sink     io.Writer
	closer   func() error
}

// close releases the output sink.
func (s *session) close() error {
	if s.closer == nil {
		return nil
	}
	return s.closer()
}

// newFlagSet builds a flag set that reports errors through the environment.
func newFlagSet(env *Environment, name string) *flag.FlagSet {
	flags := flag.NewFlagSet("caveloop "+name, flag.ContinueOnError)
	flags.SetOutput(env.Stderr)
	return flags
}

// openSession resolves configuration, output and optionally the store.
func openSession(env *Environment, common commonFlags, needStore bool) (*session, int) {
	cfg, err := config.Load(common.configPath)
	if err != nil {
		fmt.Fprintf(env.Stderr, "caveloop: %v\n", err)
		return nil, ExitError
	}
	if strings.TrimSpace(common.dataDir) != "" {
		cfg = cfg.WithDataDir(common.dataDir)
	}
	cfg, err = cfg.WithFormat(common.format)
	if err != nil {
		fmt.Fprintf(env.Stderr, "caveloop: %v\n", err)
		return nil, ExitError
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(env.Stderr, "caveloop: %v\n", err)
		return nil, ExitError
	}
	current := &session{cfg: cfg, sink: env.Stdout}
	if strings.TrimSpace(common.outPath) != "" {
		if err := os.MkdirAll(filepath.Dir(common.outPath), 0o755); err != nil {
			fmt.Fprintf(env.Stderr, "caveloop: preparing output directory: %v\n", err)
			return nil, ExitError
		}
		handle, err := os.Create(common.outPath)
		if err != nil {
			fmt.Fprintf(env.Stderr, "caveloop: creating output file: %v\n", err)
			return nil, ExitError
		}
		current.sink = handle
		current.closer = handle.Close
	}
	current.renderer = report.New(current.sink, cfg)
	if needStore {
		dir, err := cfg.ResolvedDataDir()
		if err != nil {
			_ = current.close()
			fmt.Fprintf(env.Stderr, "caveloop: %v\n", err)
			return nil, ExitError
		}
		handle, err := store.Open(dir)
		if err != nil {
			_ = current.close()
			fmt.Fprintf(env.Stderr, "caveloop: %v\n", err)
			return nil, ExitError
		}
		current.store = handle
	}
	return current, ExitOK
}

// fail reports an error on stderr and returns the requested code.
func fail(env *Environment, code int, err error) int {
	fmt.Fprintf(env.Stderr, "caveloop: %v\n", err)
	return code
}

// parseFlags parses a subcommand flag set and rejects positional arguments. The
// second result is false when the caller must stop and return the code as is,
// which covers both a flag error and an explicit help request.
func parseFlags(env *Environment, flags *flag.FlagSet, args []string) (int, bool) {
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK, false
		}
		return ExitError, false
	}
	if flags.NArg() > 0 {
		fmt.Fprintf(env.Stderr, "caveloop: unexpected argument %q\n", flags.Arg(0))
		return ExitError, false
	}
	return ExitOK, true
}

// summariseCodes renders the distinct issue codes of a list for stderr hints.
func summariseCodes(codes map[string]int) string {
	if len(codes) == 0 {
		return "none"
	}
	keys := make([]string, 0, len(codes))
	for key := range codes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", key, codes[key]))
	}
	return strings.Join(parts, " ")
}
