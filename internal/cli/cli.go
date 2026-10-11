// Package cli implements OwnScout's stable command-line experience.
package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	g := findGroup(args[0])
	if g == nil {
		return usageFailure(out, "unknown command "+quote(args[0]), "ownscout --help")
	}
	return g.run(args[1:], out)
}

func doctor(out io.Writer) int {
	writeHuman(out, "OwnScout doctor: ok\n\nChecks:\n  ✓ CLI is available\n  ✓ local-only mode\n  ✓ repository mutation disabled\n\nNext action: run a contract validation or evidence verification.")
	return 0
}

func printVersion(out io.Writer) int {
	writeHuman(out, "ownscout "+version)
	return 0
}

func nodeBind(flags map[string]string) (resultData, int) {
	packet, violations, err := loadPacket(flags["--packet"])
	if err != nil {
		return commandError("node bind", "packet could not be loaded", err.Error(), "Provide a readable packet file with --packet <file>."), 2
	}
	if isDecodeFailure(violations) {
		return commandError("node bind", "packet could not be decoded", "strict packet decoding failed", "Provide one valid packet-v1 JSON object with --packet <file>."), 2
	}
	if len(violations) != 0 {
		return resultData{
			Command:    "node bind",
			OK:         false,
			Summary:    fmt.Sprintf("packet contract failed (%d violation(s))", len(violations)),
			Details:    packetViolationDetails(violations),
			NextAction: "Fix the packet contract, then run node binding again.",
		}, 1
	}

	binding, err := node.CanonicalPacketBinding(packet)
	if err != nil {
		return commandError("node bind", "packet binding could not be computed", "canonical packet binding failed", "Provide a valid packet-v1 document and try again."), 2
	}
	return resultData{
		Command:    "node bind",
		OK:         true,
		Summary:    "packet binding computed",
		Details:    []string{binding},
		NextAction: "Use this as packet_binding_sha256 in a node-envelope-v1 document.",
	}, 0
}

func nodeVerify(flags map[string]string) (resultData, int) {
	return verifyNodeEnvelope(flags["--repo"], flags["--packet"], flags["--envelope"], flags["--ledger"], flags["--relocate"] != "")
}

func ledgerVerify(flags map[string]string) (resultData, int) {
	summary, err := ledger.Verify(flags["--ledger"])
	if err != nil {
		var validationErr *ledger.ValidationError
		if errors.As(err, &validationErr) {
			return resultData{
				Command:    "ledger verify",
				OK:         false,
				Summary:    "ledger verification failed",
				Details:    []string{err.Error()},
				NextAction: "Repair the ledger, then run ledger verification again.",
			}, 1
		}
		return resultData{
			Command:    "ledger verify",
			OK:         false,
			Summary:    "ledger could not be read",
			Details:    []string{err.Error()},
			NextAction: "Provide a readable ledger file with --ledger <file>.",
		}, 2
	}
	tip := summary.Tip
	if tip == "" {
		tip = "none"
	}
	return resultData{
		Command:    "ledger verify",
		OK:         true,
		Summary:    "ledger is intact",
		Details:    []string{fmt.Sprintf("%d record(s), tip %s", summary.Records, tip)},
		NextAction: "The ledger chain is intact.",
	}, 0
}

func ledgerRotate(flags map[string]string) (resultData, int) {
	summary, archivePath, err := ledger.Rotate(flags["--ledger"])
	if err != nil {
		var validationErr *ledger.ValidationError
		if errors.As(err, &validationErr) {
			return resultData{
				Command:    "ledger rotate",
				OK:         false,
				Summary:    "ledger verification failed",
				Details:    []string{err.Error()},
				NextAction: "Repair the ledger before rotating; a broken chain keeps its live name.",
			}, 1
		}
		return resultData{
			Command:    "ledger rotate",
			OK:         false,
			Summary:    "ledger could not be rotated",
			Details:    []string{err.Error()},
			NextAction: "Provide a readable, non-empty ledger file with --ledger <file>.",
		}, 2
	}
	return resultData{
		Command:    "ledger rotate",
		OK:         true,
		Summary:    "ledger rotated",
		Details:    []string{fmt.Sprintf("%d record(s), tip %s", summary.Records, summary.Tip), fmt.Sprintf("archived to %s", archivePath)},
		NextAction: "The next node verify starts a fresh chain; audit the archive with ownscout ledger verify.",
	}, 0
}

// verifyNodeEnvelope is deliberately a data-producing function. The ledger
// close is deferred until every path has been decided, while output is emitted
// only after that close has succeeded or produced its own error.
func verifyNodeEnvelope(repoPath, packetPath, envelopePath, ledgerPath string, relocateOption ...bool) (data resultData, code int) {
	relocate := len(relocateOption) > 0 && relocateOption[0]
	packetBytes, err := readBounded(packetPath, "packet", nodepacket.MaxInputBytes)
	if err != nil {
		return nodeError("packet could not be loaded", err.Error(), "Provide a readable packet file with --packet <file>."), 2
	}
	envelopeBytes, err := readBounded(envelopePath, "envelope", nodepacket.MaxInputBytes)
	if err != nil {
		return nodeError("envelope could not be loaded", err.Error(), "Provide a readable envelope file with --envelope <file>."), 2
	}

	packet, violations := nodepacket.DecodeValid(packetBytes)
	if len(violations) != 0 {
		if isDecodeFailure(violations) {
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

	// Resolve the repository before the ledger opens it: a repo error reported
	// here keeps the repository label instead of surfacing as a ledger failure.
	if _, err := evidence.ResolveRoot(repoPath); err != nil {
		return nodeError("repository could not be checked", err.Error(), "Provide a readable repository directory with --repo <dir>."), 2
	}
	store, err := ledger.Open(ledgerPath, repoPath)
	if err != nil {
		var full *ledger.FullError
		if errors.As(err, &full) {
			return ledgerFullError(full), 2
		}
		return nodeError("ledger could not be opened", "ledger open failed", "Provide a writable ledger path outside the repository and try again."), 2
	}
	defer func() {
		if closeErr := store.Close(); closeErr != nil {
			data = nodeError("ledger could not be closed", "ledger close failed", "Check the ledger path and try again.")
			code = 2
		}
	}()

	report, err := evidence.VerifyPacketWithOptions(repoPath, packet, evidence.Options{Relocate: relocate})
	if err != nil {
		return nodeError("repository could not be checked", err.Error(), "Provide a readable repository directory with --repo <dir>."), 2
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
		var full *ledger.FullError
		if errors.As(err, &full) {
			return ledgerFullError(full), 2
		}
		return nodeError("ledger append failed", "node results could not be appended", "Check the ledger and try again; no result was consumed."), 2
	}

	details := nodeResultDetails(evaluation.Results)
	if !evaluation.OK {
		if relocate {
			details = append(details, evidenceIssues(report)...)
		}
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

func commandError(command, summary, detail, next string) resultData {
	return resultData{Command: command, OK: false, Summary: summary, Details: []string{detail}, NextAction: next}
}

func ledgerFullError(err *ledger.FullError) resultData {
	return nodeError("ledger is full", err.Error(), "Archive the full ledger aside (mv) and rerun to start a fresh chain; audit archives with ownscout ledger verify.")
}

func nodeError(summary, detail, next string) resultData {
	return commandError("node verify", summary, detail, next)
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

func contractValidate(flags map[string]string) (resultData, int) {
	packet, violations, err := loadPacket(flags["--packet"])
	if err != nil {
		return resultData{
			Command:    "contract validate",
			OK:         false,
			Summary:    "packet could not be loaded",
			Details:    []string{err.Error()},
			NextAction: "Provide a readable JSON packet with --packet <file>.",
		}, 2
	}
	if isDecodeFailure(violations) {
		return resultData{
			Command:    "contract validate",
			OK:         false,
			Summary:    "packet could not be decoded",
			Details:    []string{"strict packet decoding failed"},
			NextAction: "Provide one valid packet-v1 JSON object with --packet <file>.",
		}, 2
	}
	if len(violations) > 0 {
		return resultData{
			Command:    "contract validate",
			OK:         false,
			Summary:    fmt.Sprintf("packet is invalid (%d violation(s))", len(violations)),
			Details:    packetViolationDetails(violations),
			NextAction: "Fix the listed packet fields, then run contract validation again.",
		}, 1
	}
	evaluation := contract.EvaluatePacket(packet)
	return resultData{
		Command:    "contract validate",
		OK:         true,
		Summary:    "packet is valid",
		Details:    []string{"outcome: " + packet.Outcome, "default action: " + evaluation.Action},
		NextAction: "Run evidence verification before consuming this packet.",
	}, 0
}

func evidenceVerify(flags map[string]string) (resultData, int) {
	packet, violations, err := loadPacket(flags["--packet"])
	if err != nil {
		return resultData{"evidence verify", false, "packet could not be loaded", []string{err.Error()}, "Provide a readable JSON packet with --packet <file>."}, 2
	}
	if isDecodeFailure(violations) {
		return resultData{"evidence verify", false, "packet could not be decoded", []string{"strict packet decoding failed"}, "Provide one valid packet-v1 JSON object with --packet <file>."}, 2
	}
	if len(violations) > 0 {
		return resultData{"evidence verify", false, "packet is invalid", packetViolationDetails(violations), "Fix the packet contract, then verify evidence again."}, 1
	}
	report, err := evidence.VerifyPacketWithOptions(flags["--repo"], packet, evidence.Options{Relocate: flags["--relocate"] != ""})
	if err != nil {
		return resultData{"evidence verify", false, "repository could not be checked", []string{err.Error()}, "Provide a readable repository directory with --repo <dir>."}, 2
	}
	issues := evidenceIssues(report)
	if !report.Ok {
		return resultData{"evidence verify", false, fmt.Sprintf("evidence verification failed (%d issue(s))", len(issues)), issues, "Refresh or correct the listed evidence, then verify again."}, 1
	}
	return resultData{"evidence verify", true, "evidence verified", []string{fmt.Sprintf("verified %d evidence span(s)", report.VerifiedCount)}, "The packet is ready for its declared default action."}, 0
}

func hasHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
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
