// Package cli implements OwnScout's stable command-line experience.
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"ownscout/internal/contract"
	"ownscout/internal/evidence"
	"ownscout/internal/ledger"
	"ownscout/internal/node"
	"ownscout/internal/nodepacket"
)

// version can be overridden at release build time with:
// -ldflags "-X ownscout/internal/cli.version=<version>"
var version = "0.1.0"

const rootUsage = `OwnScout — local repository evidence checks

Usage:
  ownscout doctor
  ownscout version
  ownscout contract validate --packet <file> [--json]
  ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]
  ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]

Use "ownscout <command> --help" for command details.`

// Run executes args and writes exactly one human or JSON result. It returns the
// documented process exit code.
func Run(args []string, out io.Writer) int {
	if len(args) == 0 {
		writeHuman(out, "error: a command is required\n\n"+rootUsage+"\n\nNext action: run 'ownscout --help'.")
		return 2
	}
	if hasHelp(args) {
		return writeHelp(args, out)
	}

	switch args[0] {
	case "doctor":
		if len(args) != 1 {
			return usageFailure(out, "doctor does not accept arguments", "ownscout doctor --help")
		}
		writeHuman(out, "OwnScout doctor: ok\n\nChecks:\n  ✓ CLI is available\n  ✓ local-only mode\n  ✓ repository mutation disabled\n\nNext action: run a contract validation or evidence verification.")
		return 0
	case "version":
		if len(args) != 1 {
			return usageFailure(out, "version does not accept arguments", "ownscout version --help")
		}
		writeHuman(out, "ownscout "+version)
		return 0
	case "contract":
		return runContract(args[1:], out)
	case "evidence":
		return runEvidence(args[1:], out)
	case "node":
		return runNode(args[1:], out)
	default:
		return usageFailure(out, "unknown command "+quote(args[0]), "ownscout --help")
	}
}

func runNode(args []string, out io.Writer) int {
	if len(args) == 0 {
		return usageFailureWithJSON(out, "a node subcommand is required", "ownscout node --help", false)
	}
	if args[0] != "verify" {
		return usageFailureWithJSON(out, "unknown node subcommand "+quote(args[0]), "ownscout node --help", hasJSON(args))
	}
	flags, jsonOutput, err := parseFlags(args[1:], map[string]bool{
		"--repo":     true,
		"--packet":   true,
		"--envelope": true,
		"--ledger":   true,
	})
	if err != nil {
		return usageFailureWithJSON(out, err.Error(), "ownscout node verify --help", hasJSON(args))
	}
	for _, flag := range []string{"--repo", "--packet", "--envelope", "--ledger"} {
		if flags[flag] == "" {
			return usageFailureWithJSON(out, "missing required "+flag+" value", "ownscout node verify --help", jsonOutput)
		}
	}

	data, code := verifyNodeEnvelope(flags["--repo"], flags["--packet"], flags["--envelope"], flags["--ledger"])
	return result(out, jsonOutput, data, code)
}

// verifyNodeEnvelope is deliberately a data-producing function. The ledger
// close is deferred until every path has been decided, while output is emitted
// only after that close has succeeded or produced its own error.
func verifyNodeEnvelope(repoPath, packetPath, envelopePath, ledgerPath string) (data resultData, code int) {
	packetBytes, err := readBounded(packetPath, "packet", nodepacket.MaxInputBytes)
	if err != nil {
		return nodeError("packet could not be loaded", "packet input could not be read", "Provide a readable packet file with --packet <file>."), 2
	}
	envelopeBytes, err := readBounded(envelopePath, "envelope", nodepacket.MaxInputBytes)
	if err != nil {
		return nodeError("envelope could not be loaded", "envelope input could not be read", "Provide a readable envelope file with --envelope <file>."), 2
	}

	packet, violations := nodepacket.DecodeValid(packetBytes)
	if len(violations) != 0 {
		if len(violations) == 1 && violations[0].Rule == nodepacket.RuleDecode {
			return nodeError("packet could not be decoded", "strict packet decoding failed", "Provide one valid packet-v1 JSON object with --packet <file>."), 2
		}
		return resultData{
			Command:    "node verify",
			OK:         false,
			Summary:    fmt.Sprintf("packet contract failed (%d violation(s))", len(violations)),
			Details:    packetViolationDetails(violations),
			NextAction: "Fix the packet contract, then run node verification again.",
		}, 2
	}

	env, err := node.ParseEnvelope(envelopeBytes)
	if err != nil {
		return nodeError("envelope could not be parsed", "strict envelope parsing failed", "Provide one valid node-envelope-v1 JSON object with --envelope <file>."), 2
	}

	binding, err := node.CanonicalPacketBinding(packet)
	if err != nil {
		return nodeError("packet binding could not be computed", "canonical packet binding failed", "Provide a valid packet-v1 document and try again."), 2
	}
	if err := node.ValidateEnvelope(env, packet, binding); err != nil {
		return nodeError("envelope validation failed", "node-envelope-v1 validation failed", "Fix the envelope binding or graph, then run node verification again."), 2
	}

	store, err := ledger.Open(ledgerPath, repoPath)
	if err != nil {
		return nodeError("ledger could not be opened", "ledger open failed", "Provide a writable ledger path outside the repository and try again."), 2
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			data = nodeError("ledger could not be closed", "ledger close failed", "Check the ledger path and try again.")
			code = 2
		}
	}()

	report, err := evidence.VerifyPacket(repoPath, packet)
	if err != nil {
		return nodeError("repository could not be checked", "repository evidence verification could not run", "Provide a readable repository directory with --repo <dir>."), 2
	}

	evaluation := node.EvaluateEnvelope(env, packet, binding, report)
	ledgerResults := make([]ledger.NodeResult, 0, len(evaluation.Results))
	for _, item := range evaluation.Results {
		ledgerResults = append(ledgerResults, ledger.NodeResult{
			NodeID: item.NodeID,
			Status: string(item.Status),
			Reason: item.Reason,
		})
	}

	envelopeSum := sha256.Sum256(envelopeBytes)
	envelopeHash := hex.EncodeToString(envelopeSum[:])
	if _, err := store.Append(envelopeHash, binding, version, ledgerResults); err != nil {
		return nodeError("ledger append failed", "node results could not be appended", "Check the ledger and try again; no result was consumed."), 2
	}

	details := nodeResultDetails(evaluation.Results)
	if !evaluation.OK {
		return resultData{
			Command:    "node verify",
			OK:         false,
			Summary:    "node evaluation failed",
			Details:    details,
			NextAction: "Refresh or correct the failed evidence, then run node verification again.",
		}, 1
	}
	return resultData{
		Command:    "node verify",
		OK:         true,
		Summary:    "all nodes are evidence_current",
		Details:    details,
		NextAction: "The node envelope is recorded and ready for its declared workflow.",
	}, 0
}

func nodeError(summary, detail, next string) resultData {
	return resultData{Command: "node verify", OK: false, Summary: summary, Details: []string{detail}, NextAction: next}
}

func nodeResultDetails(results []node.Result) []string {
	details := make([]string, 0, len(results))
	for _, item := range results {
		detail := fmt.Sprintf("node %q: %s", item.NodeID, item.Status)
		if item.Reason != "" {
			detail += " (" + item.Reason + ")"
		}
		details = append(details, detail)
	}
	return details
}

func runContract(args []string, out io.Writer) int {
	if len(args) == 0 {
		return usageFailureWithJSON(out, "a contract subcommand is required", "ownscout contract --help", false)
	}
	if args[0] != "validate" {
		return usageFailureWithJSON(out, "unknown contract subcommand "+quote(args[0]), "ownscout contract --help", hasJSON(args))
	}
	packetPath, jsonOutput, err := parseFlags(args[1:], map[string]bool{"--packet": true})
	if err != nil {
		return usageFailureWithJSON(out, err.Error(), "ownscout contract validate --help", hasJSON(args))
	}
	path, ok := packetPath["--packet"]
	if !ok || path == "" {
		return usageFailureWithJSON(out, "missing required --packet <file>", "ownscout contract validate --help", jsonOutput)
	}

	packet, err := loadPacket(path)
	if err != nil {
		return result(out, jsonOutput, resultData{
			Command:    "contract validate",
			OK:         false,
			Summary:    "packet could not be loaded",
			Details:    []string{err.Error()},
			NextAction: "Provide a readable JSON packet with --packet <file>.",
		}, 2)
	}
	violations := validatePacket(packet)
	if len(violations) > 0 {
		return result(out, jsonOutput, resultData{
			Command:    "contract validate",
			OK:         false,
			Summary:    fmt.Sprintf("packet is invalid (%d violation(s))", len(violations)),
			Details:    violations,
			NextAction: "Fix the listed packet fields, then run contract validation again.",
		}, 1)
	}
	evaluation := contract.EvaluatePacket(packet)
	return result(out, jsonOutput, resultData{
		Command:    "contract validate",
		OK:         true,
		Summary:    "packet is valid",
		Details:    []string{"outcome: " + packet.Outcome, "default action: " + evaluation.Action},
		NextAction: "Run evidence verification before consuming this packet.",
	}, 0)
}

func runEvidence(args []string, out io.Writer) int {
	if len(args) == 0 {
		return usageFailureWithJSON(out, "an evidence subcommand is required", "ownscout evidence --help", false)
	}
	if args[0] != "verify" {
		return usageFailureWithJSON(out, "unknown evidence subcommand "+quote(args[0]), "ownscout evidence --help", hasJSON(args))
	}
	flags, jsonOutput, err := parseFlags(args[1:], map[string]bool{"--repo": true, "--packet": true}, "--relocate")
	if err != nil {
		return usageFailureWithJSON(out, err.Error(), "ownscout evidence verify --help", hasJSON(args))
	}
	repo, repoOK := flags["--repo"]
	packetPath, packetOK := flags["--packet"]
	if !repoOK || repo == "" || !packetOK || packetPath == "" {
		return usageFailureWithJSON(out, "both --repo <dir> and --packet <file> are required", "ownscout evidence verify --help", jsonOutput)
	}
	packet, err := loadPacket(packetPath)
	if err != nil {
		return result(out, jsonOutput, resultData{"evidence verify", false, "packet could not be loaded", []string{err.Error()}, "Provide a readable JSON packet with --packet <file>."}, 2)
	}
	if violations := validatePacket(packet); len(violations) > 0 {
		return result(out, jsonOutput, resultData{"evidence verify", false, "packet is invalid", violations, "Fix the packet contract, then verify evidence again."}, 1)
	}
	report, err := evidence.VerifyPacketWithOptions(repo, packet, evidence.Options{Relocate: flags["--relocate"] != ""})
	if err != nil {
		return result(out, jsonOutput, resultData{"evidence verify", false, "repository could not be checked", []string{err.Error()}, "Provide a readable repository directory with --repo <dir>."}, 2)
	}
	issues := evidenceIssues(report)
	if !report.Ok {
		return result(out, jsonOutput, resultData{"evidence verify", false, fmt.Sprintf("evidence verification failed (%d issue(s))", len(issues)), issues, "Refresh or correct the listed evidence, then verify again."}, 1)
	}
	return result(out, jsonOutput, resultData{"evidence verify", true, "evidence verified", []string{fmt.Sprintf("verified %d evidence span(s)", report.VerifiedCount)}, "The packet is ready for its declared default action."}, 0)
}

func hasHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func writeHelp(args []string, out io.Writer) int {
	text := rootUsage
	if len(args) >= 1 {
		switch args[0] {
		case "doctor":
			text = "Usage: ownscout doctor\n\nChecks that the local CLI is ready.\n\nNext action: run this command without additional arguments."
		case "version":
			text = "Usage: ownscout version\n\nPrints the OwnScout version."
		case "contract":
			text = "Usage: ownscout contract validate --packet <file> [--json]\n\nValidates packet structure and outcome rules."
		case "evidence":
			text = "Usage: ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]\n\nVerifies packet evidence spans against a local repository."
		case "node":
			text = "Usage: ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]\n\nVerifies a node-envelope-v1 graph against fresh repository evidence and records the ordered results."
		}
	}
	if len(args) >= 2 && args[0] == "contract" && args[1] == "validate" {
		text = "Usage: ownscout contract validate --packet <file> [--json]\n\nReads and validates one JSON packet without printing its contents.\n\nNext action: provide --packet with a readable packet file."
	}
	if len(args) >= 2 && args[0] == "evidence" && args[1] == "verify" {
		text = "Usage: ownscout evidence verify --repo <dir> --packet <file> [--relocate] [--json]\n\nValidates the packet, then checks each evidence span locally. With --relocate, a failed span is also searched for the recorded content fingerprint and the failure names where that content now lives.\n\nNext action: provide both paths and rerun."
	}
	if len(args) >= 2 && args[0] == "node" && args[1] == "verify" {
		text = "Usage: ownscout node verify --repo <dir> --packet <file> --envelope <file> --ledger <file> [--json]\n\nStrictly validates the packet and node-envelope-v1 graph, verifies fresh evidence, evaluates in deterministic graph order, and appends every result once.\n\nNext action: provide all four paths and rerun."
	}
	writeHuman(out, text)
	return 0
}

// parseFlags parses a subcommand's flags. Flags named in valued take a value;
// flags named in boolean are switches. --json is always accepted and reported
// separately because every command renders both modes. A switch is recorded as
// "true" in the returned map, so callers test the same way they test a valued
// flag being present.
func parseFlags(args []string, valued map[string]bool, boolean ...string) (map[string]string, bool, error) {
	values := map[string]string{}
	switches := make(map[string]bool, len(boolean))
	for _, name := range boolean {
		switches[name] = true
	}
	jsonOutput := false
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--json":
			jsonOutput = true
		case switches[args[i]]:
			values[args[i]] = "true"
		default:
			if !valued[args[i]] {
				return nil, false, fmt.Errorf("unknown flag or argument %s", quote(args[i]))
			}
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
				return nil, false, fmt.Errorf("%s requires a value", args[i])
			}
			values[args[i]] = args[i+1]
			i++
		}
	}
	return values, jsonOutput, nil
}

type resultData struct {
	Command    string   `json:"command"`
	OK         bool     `json:"ok"`
	Summary    string   `json:"summary"`
	Details    []string `json:"details"`
	NextAction string   `json:"next_action"`
}

func result(out io.Writer, jsonOutput bool, data resultData, code int) int {
	if jsonOutput {
		encoded, _ := json.Marshal(data)
		fmt.Fprintln(out, string(encoded))
	} else {
		status := "OK"
		if !data.OK {
			status = "ERROR"
		}
		fmt.Fprintf(out, "%s: %s\n", status, data.Summary)
		for _, detail := range data.Details {
			fmt.Fprintf(out, "  - %s\n", detail)
		}
		fmt.Fprintf(out, "Next action: %s\n", data.NextAction)
	}
	return code
}

func usageFailure(out io.Writer, message, next string) int {
	writeHuman(out, "error: "+message+"\nNext action: run '"+next+"'.")
	return 2
}

func usageFailureWithJSON(out io.Writer, message, next string, jsonOutput bool) int {
	if jsonOutput {
		return result(out, true, resultData{
			Command:    "usage",
			OK:         false,
			Summary:    message,
			Details:    []string{"usage: " + next},
			NextAction: "Run '" + next + "'.",
		}, 2)
	}
	return usageFailure(out, message, next)
}

func hasJSON(args []string) bool {
	for _, arg := range args {
		if arg == "--json" {
			return true
		}
	}
	return false
}
func writeHuman(out io.Writer, text string) { fmt.Fprintln(out, text) }
func quote(s string) string                 { return "'" + s + "'" }
