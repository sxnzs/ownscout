package cli

import (
	"io"
	"strings"
)

// The command table is the one place that says what commands exist. Dispatch,
// flag validation, the root usage text, group help and leaf help are all derived
// from it, so a command's usage line cannot drift from what it accepts.

// group is a top-level command. It either runs directly (doctor, version) or
// dispatches to one of its leaves (contract validate, node verify, ...).
type group struct {
	name    string
	summary string // help body shown for the group
	leaves  []*leaf

	// direct and usage are set only for a group with no subcommands.
	direct func(out io.Writer) int
	usage  string
}

// leaf is a runnable subcommand.
type leaf struct {
	name     string   // subcommand name, e.g. "verify"
	usage    string   // the full usage line, shown in help and the root usage
	body     string   // help body shown below the usage line
	valued   []string // flags that take a value
	switches []string // flags that are boolean switches
	required []string // valued flags that must be present and non-empty, checked in order

	// missing replaces the default "missing required <flag> value" message when
	// the command words its missing-flag failure differently.
	missing string

	handler func(flags map[string]string) (resultData, int)
}

var commands = []*group{
	{
		name:    "doctor",
		usage:   "ownscout doctor",
		summary: "Checks that the local CLI is ready.\n\nNext action: run this command without additional arguments.",
		direct:  doctor,
	},
	{
		name:    "version",
		usage:   "ownscout version",
		summary: "Prints the OwnScout version.",
		direct:  printVersion,
	},
	{
		name:    "contract",
		summary: "Validates packet structure and outcome rules.",
		leaves: []*leaf{{
			name:     "validate",
			usage:    "ownscout contract validate --packet <file> [--json]",
			body:     "Reads and validates one JSON packet without printing its contents.\n\nNext action: provide --packet with a readable packet file.",
			valued:   []string{"--packet"},
			required: []string{"--packet"},
			missing:  "missing required --packet <file>",
			handler:  contractValidate,
		}},
	},
	{
		name:    "evidence",
		summary: "Verifies packet evidence spans against a local repository.",
		leaves: []*leaf{{
			name:     "verify",
			usage:    "ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]",
			body:     "Validates the packet, then checks each evidence span locally. With --relocate, a failed span is also searched for the recorded content fingerprint and the failure names where that content now lives.\n\nNext action: provide both paths and rerun.",
			valued:   []string{"--repo", "--packet"},
			switches: []string{"--relocate"},
			required: []string{"--repo", "--packet"},
			missing:  "both --repo <dir> and --packet <file> are required",
			handler:  evidenceVerify,
		}},
	},
	{
		name:    "ledger",
		summary: "Audits or archives an append-only ledger.",
		leaves: []*leaf{
			{
				name:     "verify",
				usage:    "ownscout ledger verify --ledger <file> [--json]",
				body:     "Replays the SHA-256 ledger chain without opening or modifying it.\n\nNext action: provide --ledger with a readable ledger file.",
				valued:   []string{"--ledger"},
				required: []string{"--ledger"},
				handler:  ledgerVerify,
			},
			{
				name:     "rotate",
				usage:    "ownscout ledger rotate --ledger <file> [--json]",
				body:     "Audits the chain, then renames the ledger to <file>.<tip8> so the next verification starts a fresh chain.\n\nNext action: provide --ledger with a readable, non-empty ledger file.",
				valued:   []string{"--ledger"},
				required: []string{"--ledger"},
				handler:  ledgerRotate,
			},
		},
	},
	{
		name:    "node",
		summary: "Binds packets or verifies a node-envelope-v1 graph against fresh repository evidence.",
		leaves: []*leaf{
			{
				name:     "bind",
				usage:    "ownscout node bind --packet <file> [--json]",
				body:     "Computes the canonical packet binding for a strictly decoded packet.\n\nNext action: provide --packet with a readable packet file.",
				valued:   []string{"--packet"},
				required: []string{"--packet"},
				handler:  nodeBind,
			},
			{
				name:     "verify",
				usage:    "ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--relocate] [--json]",
				body:     "Strictly validates the packet and node-envelope-v1 graph, verifies fresh evidence, evaluates in deterministic graph order, and appends every result once. With --relocate, failed evidence details include matching locations when available.\n\nNext action: provide all four paths and rerun.",
				valued:   []string{"--repo", "--packet", "--envelope", "--ledger"},
				switches: []string{"--relocate"},
				required: []string{"--repo", "--packet", "--envelope", "--ledger"},
				handler:  nodeVerify,
			},
		},
	},
}

var rootUsage = buildRootUsage()

func buildRootUsage() string {
	var b strings.Builder
	b.WriteString("OwnScout — local repository evidence checks\n\nUsage:")
	for _, g := range commands {
		for _, line := range g.usageLines() {
			b.WriteString("\n  " + line)
		}
	}
	b.WriteString("\n\nUse \"ownscout <command> --help\" for command details.")
	return b.String()
}

func findGroup(name string) *group {
	for _, g := range commands {
		if g.name == name {
			return g
		}
	}
	return nil
}

func (g *group) findLeaf(name string) *leaf {
	for _, l := range g.leaves {
		if l.name == name {
			return l
		}
	}
	return nil
}

func (g *group) usageLines() []string {
	if g.direct != nil {
		return []string{g.usage}
	}
	lines := make([]string, 0, len(g.leaves))
	for _, l := range g.leaves {
		lines = append(lines, l.usage)
	}
	return lines
}

func (g *group) helpText() string {
	return "Usage: " + strings.Join(g.usageLines(), "\n       ") + "\n\n" + g.summary
}

func (l *leaf) helpText() string {
	return "Usage: " + l.usage + "\n\n" + l.body
}

// run dispatches args, which follow the group name.
func (g *group) run(args []string, out io.Writer) int {
	helpRef := "ownscout " + g.name + " --help"
	if g.direct != nil {
		if len(args) != 0 {
			return usageFailure(out, g.name+" does not accept arguments", helpRef)
		}
		return g.direct(out)
	}
	if len(args) == 0 {
		return usageFailureWithJSON(out, withArticle(g.name)+" subcommand is required", helpRef, false)
	}
	l := g.findLeaf(args[0])
	if l == nil {
		return usageFailureWithJSON(out, "unknown "+g.name+" subcommand "+quote(args[0]), helpRef, hasJSON(args))
	}
	return l.run(g.name, args[1:], out)
}

// run parses a leaf's flags, enforces its required flags, runs the handler and
// writes its single result. args follow the leaf name.
func (l *leaf) run(groupName string, args []string, out io.Writer) int {
	helpRef := "ownscout " + groupName + " " + l.name + " --help"
	valued := make(map[string]bool, len(l.valued))
	for _, name := range l.valued {
		valued[name] = true
	}
	flags, jsonOutput, err := parseFlags(args, valued, l.switches...)
	if err != nil {
		return usageFailureWithJSON(out, err.Error(), helpRef, hasJSON(args))
	}
	for _, name := range l.required {
		if flags[name] == "" {
			message := l.missing
			if message == "" {
				message = "missing required " + name + " value"
			}
			return usageFailureWithJSON(out, message, helpRef, jsonOutput)
		}
	}
	data, code := l.handler(flags)
	return result(out, jsonOutput, data, code)
}

// writeHelp resolves --help for the most specific command named in args: a leaf,
// else its group, else the root usage.
func writeHelp(args []string, out io.Writer) int {
	text := rootUsage
	if g := findGroup(args[0]); g != nil {
		text = g.helpText()
		if len(args) >= 2 {
			if l := g.findLeaf(args[1]); l != nil {
				text = l.helpText()
			}
		}
	}
	writeHuman(out, text)
	return 0
}

func withArticle(noun string) string {
	if strings.ContainsRune("aeiou", rune(noun[0])) {
		return "an " + noun
	}
	return "a " + noun
}
