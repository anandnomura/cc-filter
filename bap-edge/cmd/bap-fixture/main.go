package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"bap-system/bap-edge/internal/bapedge"
)

func main() {
	mode := flag.String("mode", "verify", "manifest or verify")
	directory := flag.String("directory", "", "directory containing sanitized Claude fixtures")
	manifestPath := flag.String("manifest", "", "certification manifest path")
	bundlePath := flag.String("bundle", "", "active signed policy envelope path")
	publicKeyPath := flag.String("public-key", "", "policy-bundle verification public key")
	required := flag.String("require-models", "", "comma-separated required model families")
	flag.Parse()
	if *directory == "" || *manifestPath == "" || *bundlePath == "" || *publicKeyPath == "" {
		fatal("directory, manifest, bundle, and public-key are required")
	}
	requiredModels := splitList(*required)
	switch *mode {
	case "manifest":
		manifest, err := bapedge.BuildFixtureManifest(*directory, *bundlePath, *publicKeyPath, requiredModels, time.Now().UTC())
		if err != nil {
			fatal(err.Error())
		}
		if err := bapedge.WriteFixtureManifest(*manifestPath, manifest); err != nil {
			fatal(err.Error())
		}
		printJSON(map[string]any{"status": "manifest_written", "fixture_count": manifest.FixtureCount, "models": manifest.Models, "policy_version": manifest.PolicyVersion, "rules_digest": manifest.RulesDigest})
	case "verify":
		report, err := bapedge.VerifyFixtureManifest(*directory, *manifestPath, *bundlePath, *publicKeyPath, requiredModels)
		if err != nil {
			fatal(err.Error())
		}
		printJSON(map[string]any{"status": "certified", "fixture_count": report.FixtureCount, "scenarios": report.Scenarios, "models": report.Models})
	default:
		fatal("mode must be manifest or verify")
	}
}

func splitList(value string) []string {
	result := []string{}
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func printJSON(value any) {
	_ = json.NewEncoder(os.Stdout).Encode(value)
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "BAP fixture certification failed:", message)
	os.Exit(1)
}
