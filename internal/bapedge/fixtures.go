package bapedge

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"cc-filter/internal/authzen"
	"cc-filter/internal/grants"
	"cc-filter/internal/policybundle"
)

const FixtureSchemaVersion = 1

var fixtureLabel = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

type ClaudeFixture struct {
	SchemaVersion     int            `json:"schema_version"`
	CapturedAt        time.Time      `json:"captured_at"`
	Scenario          string         `json:"scenario"`
	ClaudeCodeVersion string         `json:"claude_code_version"`
	Model             string         `json:"model"`
	OperatingSystem   string         `json:"operating_system"`
	HookEvent         string         `json:"hook_event"`
	Tool              string         `json:"tool"`
	ToolKnown         bool           `json:"tool_known"`
	HookSchema        map[string]any `json:"hook_schema"`
	HookSchemaDigest  string         `json:"hook_schema_digest"`
	InputSchema       map[string]any `json:"input_schema"`
	InputSchemaDigest string         `json:"input_schema_digest"`
	Action            string         `json:"action"`
	ExpectedDecision  string         `json:"expected_decision"`
	ActualDecision    string         `json:"actual_decision"`
	ReasonCode        string         `json:"reason_code"`
	RuleIDs           []string       `json:"rule_ids,omitempty"`
	PolicyVersion     uint64         `json:"policy_version"`
	RulesDigest       string         `json:"rules_digest"`
	Protected         bool           `json:"protected"`
	OutsideWorkspace  bool           `json:"outside_workspace"`
	SecurityControl   bool           `json:"security_control"`
	Destructive       bool           `json:"destructive"`
	Privileged        bool           `json:"privileged"`
	Exfiltration      bool           `json:"exfiltration"`
	Obfuscated        bool           `json:"obfuscated"`
}

type FixtureManifestEntry struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

type FixtureManifest struct {
	SchemaVersion  int                    `json:"schema_version"`
	CertifiedAt    time.Time              `json:"certified_at"`
	Status         string                 `json:"status"`
	PolicyVersion  uint64                 `json:"policy_version"`
	RulesDigest    string                 `json:"rules_digest"`
	ClaudeVersions []string               `json:"claude_code_versions"`
	Models         []string               `json:"models"`
	FixtureCount   int                    `json:"fixture_count"`
	RequiredModels []string               `json:"required_models,omitempty"`
	Fixtures       []FixtureManifestEntry `json:"fixtures"`
}

type FixtureVerification struct {
	FixtureCount int
	Scenarios    int
	Models       []string
}

func RecordFixtureFromEnvironment(raw []byte, input HookInput, request authzen.EvaluationRequest, actualDecision, reasonCode string, ruleIDs []string, bundle policybundle.Bundle) error {
	directory := strings.TrimSpace(os.Getenv("BAP_FIXTURE_CAPTURE_DIRECTORY"))
	if directory == "" || input.HookEventName != "PreToolUse" {
		return nil
	}
	scenario := strings.TrimSpace(os.Getenv("BAP_FIXTURE_SCENARIO"))
	model := strings.TrimSpace(os.Getenv("BAP_FIXTURE_MODEL"))
	clientVersion := strings.TrimSpace(os.Getenv("BAP_FIXTURE_CLAUDE_VERSION"))
	expected := strings.ToLower(strings.TrimSpace(os.Getenv("BAP_FIXTURE_EXPECTED_DECISION")))
	if !fixtureLabel.MatchString(scenario) || model == "" || clientVersion == "" || (expected != "allow" && expected != "deny") {
		return errors.New("fixture capture requires a safe scenario, model, Claude version, and expected allow/deny decision")
	}
	var rawObject map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&rawObject); err != nil {
		return fmt.Errorf("capture hook schema: %w", err)
	}
	hookSchema, ok := schemaShape(rawObject).(map[string]any)
	if !ok {
		return errors.New("hook payload schema is not an object")
	}
	inputSchema, ok := schemaShape(input.ToolInput).(map[string]any)
	if !ok {
		return errors.New("tool input schema is not an object")
	}
	fixture := ClaudeFixture{
		SchemaVersion: FixtureSchemaVersion, CapturedAt: time.Now().UTC(), Scenario: scenario,
		ClaudeCodeVersion: clientVersion, Model: model, OperatingSystem: runtime.GOOS,
		HookEvent: input.HookEventName, Tool: input.ToolName, ToolKnown: request.Action.Name != "tool.unknown",
		HookSchema: hookSchema, HookSchemaDigest: shapeDigest(hookSchema), InputSchema: inputSchema, InputSchemaDigest: shapeDigest(inputSchema),
		Action: request.Action.Name, ExpectedDecision: expected, ActualDecision: actualDecision, ReasonCode: reasonCode,
		RuleIDs: append([]string(nil), ruleIDs...), PolicyVersion: bundle.Version, RulesDigest: bundle.RulesDigest,
		Protected: boolProperty(request, "protected"), OutsideWorkspace: boolProperty(request, "outsideWorkspace"),
		SecurityControl: boolProperty(request, "securityControl"), Destructive: boolProperty(request, "destructive"),
		Privileged: boolProperty(request, "privileged"), Exfiltration: boolProperty(request, "exfiltration"), Obfuscated: boolProperty(request, "obfuscated"),
	}
	if err := ValidateClaudeFixture(fixture); err != nil {
		return err
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(fixture, "", "  ")
	if err != nil {
		return err
	}
	modelDigest := sha256.Sum256([]byte(model))
	name := safeFixtureName(scenario) + "--" + safeFixtureName(model) + "-" + hex.EncodeToString(modelDigest[:6]) + ".json"
	return atomicWrite(filepath.Join(directory, name), append(data, '\n'))
}

func ValidateClaudeFixture(fixture ClaudeFixture) error {
	if fixture.SchemaVersion != FixtureSchemaVersion || !fixtureLabel.MatchString(fixture.Scenario) || fixture.ClaudeCodeVersion == "" || fixture.Model == "" {
		return errors.New("fixture identity or schema is invalid")
	}
	if fixture.HookEvent != "PreToolUse" || fixture.Tool == "" || !fixture.ToolKnown || fixture.Action == "" || fixture.Action == "tool.unknown" {
		return errors.New("unknown or malformed tool schemas cannot be certified")
	}
	if fixture.ExpectedDecision != fixture.ActualDecision || (fixture.ActualDecision != "allow" && fixture.ActualDecision != "deny") {
		return errors.New("captured decision does not match the reviewed expectation")
	}
	if fixture.ReasonCode == "" || fixture.PolicyVersion == 0 || fixture.RulesDigest == "" {
		return errors.New("fixture decision is not bound to policy identity")
	}
	if shapeDigest(fixture.HookSchema) != fixture.HookSchemaDigest || shapeDigest(fixture.InputSchema) != fixture.InputSchemaDigest {
		return errors.New("fixture schema digest mismatch")
	}
	return nil
}

func BuildFixtureManifest(directory, bundlePath, publicKeyPath string, requiredModels []string, now time.Time) (FixtureManifest, error) {
	bundle, err := readVerifiedBundle(bundlePath, publicKeyPath, now)
	if err != nil {
		return FixtureManifest{}, err
	}
	fixtures, entries, err := readFixtureDirectory(directory)
	if err != nil {
		return FixtureManifest{}, err
	}
	if len(fixtures) == 0 {
		return FixtureManifest{}, errors.New("no captured fixtures found")
	}
	versions, models := map[string]bool{}, map[string]bool{}
	for _, fixture := range fixtures {
		if err := ValidateClaudeFixture(fixture); err != nil {
			return FixtureManifest{}, fmt.Errorf("fixture %s/%s: %w", fixture.Scenario, fixture.Model, err)
		}
		if fixture.PolicyVersion != bundle.Version || fixture.RulesDigest != bundle.RulesDigest {
			return FixtureManifest{}, fmt.Errorf("fixture %s is bound to a different policy", fixture.Scenario)
		}
		if err := ReplayClaudeFixture(fixture, bundle); err != nil {
			return FixtureManifest{}, fmt.Errorf("fixture %s replay: %w", fixture.Scenario, err)
		}
		versions[fixture.ClaudeCodeVersion], models[fixture.Model] = true, true
	}
	manifest := FixtureManifest{
		SchemaVersion: FixtureSchemaVersion, CertifiedAt: now.UTC(), Status: "certified",
		PolicyVersion: bundle.Version, RulesDigest: bundle.RulesDigest, ClaudeVersions: sortedKeys(versions), Models: sortedKeys(models),
		FixtureCount: len(fixtures), RequiredModels: normalizedRequired(requiredModels), Fixtures: entries,
	}
	if err := verifyEquivalence(fixtures, manifest.RequiredModels); err != nil {
		return FixtureManifest{}, err
	}
	return manifest, nil
}

func VerifyFixtureManifest(directory, manifestPath, bundlePath, publicKeyPath string, requiredModels []string) (FixtureVerification, error) {
	manifest, err := readManifest(manifestPath)
	if err != nil {
		return FixtureVerification{}, err
	}
	bundle, err := readVerifiedBundle(bundlePath, publicKeyPath, time.Now().UTC())
	if err != nil {
		return FixtureVerification{}, err
	}
	if manifest.SchemaVersion != FixtureSchemaVersion || manifest.Status != "certified" || manifest.PolicyVersion != bundle.Version || manifest.RulesDigest != bundle.RulesDigest {
		return FixtureVerification{}, errors.New("certification manifest is stale or bound to another policy")
	}
	_, diskEntries, err := readFixtureDirectory(directory)
	if err != nil {
		return FixtureVerification{}, err
	}
	if len(diskEntries) != len(manifest.Fixtures) {
		return FixtureVerification{}, errors.New("capture directory contains missing or unlisted fixtures")
	}
	for index := range diskEntries {
		if diskEntries[index].File != manifest.Fixtures[index].File {
			return FixtureVerification{}, errors.New("capture directory contains missing or unlisted fixtures")
		}
	}
	fixtures := make([]ClaudeFixture, 0, len(manifest.Fixtures))
	for _, entry := range manifest.Fixtures {
		if filepath.Base(entry.File) != entry.File || entry.File == "" {
			return FixtureVerification{}, errors.New("manifest contains an unsafe fixture path")
		}
		data, err := os.ReadFile(filepath.Join(directory, entry.File))
		if err != nil {
			return FixtureVerification{}, err
		}
		if digestBytes(data) != entry.SHA256 {
			return FixtureVerification{}, fmt.Errorf("fixture hash mismatch: %s", entry.File)
		}
		fixture, err := decodeFixture(data)
		if err != nil {
			return FixtureVerification{}, fmt.Errorf("fixture %s: %w", entry.File, err)
		}
		if err := ValidateClaudeFixture(fixture); err != nil {
			return FixtureVerification{}, fmt.Errorf("fixture %s: %w", entry.File, err)
		}
		if fixture.PolicyVersion != bundle.Version || fixture.RulesDigest != bundle.RulesDigest {
			return FixtureVerification{}, fmt.Errorf("fixture %s has stale policy identity", entry.File)
		}
		if err := ReplayClaudeFixture(fixture, bundle); err != nil {
			return FixtureVerification{}, fmt.Errorf("fixture %s replay: %w", entry.File, err)
		}
		fixtures = append(fixtures, fixture)
	}
	if len(fixtures) != manifest.FixtureCount || len(fixtures) == 0 {
		return FixtureVerification{}, errors.New("manifest fixture count mismatch")
	}
	required := normalizedRequired(requiredModels)
	if len(required) == 0 {
		required = manifest.RequiredModels
	}
	if err := verifyEquivalence(fixtures, required); err != nil {
		return FixtureVerification{}, err
	}
	scenarios, models := map[string]bool{}, map[string]bool{}
	for _, fixture := range fixtures {
		scenarios[fixture.Scenario], models[fixture.Model] = true, true
	}
	return FixtureVerification{FixtureCount: len(fixtures), Scenarios: len(scenarios), Models: sortedKeys(models)}, nil
}

func WriteFixtureManifest(path string, manifest FixtureManifest) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(path, append(data, '\n'))
}

// ReplayClaudeFixture regenerates privacy-safe representative values from the
// captured schema, then runs the current normalizer and signed local policy.
// Captured commands, paths, prompts, URLs, and arguments are never persisted.
func ReplayClaudeFixture(fixture ClaudeFixture, bundle policybundle.Bundle) error {
	generated, ok := generatedSchemaObject(fixture.InputSchema, fixture).(map[string]any)
	if !ok {
		return errors.New("input schema cannot be regenerated as an object")
	}
	request, err := NormalizeWithPolicy(HookInput{
		HookEventName: "PreToolUse", SessionID: "certification-replay", ToolUseID: "fixture",
		CWD: ".", ToolName: fixture.Tool, ToolInput: generated,
	}, "claude-code-local", "fixture", NormalizationPolicy{
		Profile: bundle.PolicyProfile, AllowedNetworkDomains: bundle.AllowedNetwork,
		ApprovedMCPTools: bundle.ApprovedMCP, ApprovedSubagentTypes: bundle.ApprovedDelegates,
	})
	if err != nil {
		return err
	}
	if request.Action.Name != fixture.Action {
		return fmt.Errorf("normalized action %s does not match captured action %s", request.Action.Name, fixture.Action)
	}
	if fixture.ReasonCode == "LOCAL_FILTER_DENY" {
		return nil
	}
	now := bundle.IssuedAt.Add(time.Second)
	decision, err := policybundle.Authorize(bundle, request, now)
	if err != nil {
		return err
	}
	actual := "deny"
	if decision.Allowed {
		actual = "allow"
	}
	if actual != fixture.ActualDecision || decision.ReasonCode != fixture.ReasonCode {
		return fmt.Errorf("decision/reason %s/%s does not match captured %s/%s", actual, decision.ReasonCode, fixture.ActualDecision, fixture.ReasonCode)
	}
	return nil
}

func schemaShape(value any) any {
	switch typed := value.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case json.Number, float64, float32, int, int64, int32, uint, uint64, uint32:
		return "number"
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			result[key] = schemaShape(child)
		}
		return result
	case []any:
		if len(typed) == 0 {
			return map[string]any{"type": "array", "items": "empty"}
		}
		return map[string]any{"type": "array", "items": schemaShape(typed[0])}
	default:
		return fmt.Sprintf("unsupported:%T", value)
	}
}

func generatedSchemaObject(shape any, fixture ClaudeFixture) any {
	switch typed := shape.(type) {
	case string:
		switch typed {
		case "string":
			return "fixture"
		case "boolean":
			return false
		case "number":
			return float64(1)
		case "null":
			return nil
		default:
			return "fixture"
		}
	case map[string]any:
		if typed["type"] == "array" {
			if typed["items"] == "empty" {
				return []any{}
			}
			return []any{generatedSchemaObject(typed["items"], fixture)}
		}
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			result[key] = generatedSchemaValue(key, child, fixture)
		}
		return result
	case []any:
		if len(typed) == 0 {
			return []any{}
		}
		return []any{generatedSchemaObject(typed[0], fixture)}
	default:
		return "fixture"
	}
}

func generatedSchemaValue(key string, shape any, fixture ClaudeFixture) any {
	switch key {
	case "command":
		return representativeCommand(fixture)
	case "file_path", "notebook_path":
		if fixture.Protected {
			return ".env"
		}
		if fixture.OutsideWorkspace {
			return "../outside-fixture.txt"
		}
		if fixture.SecurityControl {
			return ".claude/settings.json"
		}
		return "src/fixture.txt"
	case "path":
		if fixture.OutsideWorkspace {
			return "../outside-fixture"
		}
		return "src"
	case "url":
		return "https://fixture.invalid/resource"
	case "questions":
		return []any{map[string]any{"question": "Fixture question?"}}
	case "todos":
		return []any{map[string]any{"content": "fixture"}}
	}
	return generatedSchemaObject(shape, fixture)
}

func representativeCommand(fixture ClaudeFixture) string {
	if fixture.Obfuscated {
		return "powershell -EncodedCommand ZQBjAGgAbwA="
	}
	if fixture.Destructive {
		return "git reset --hard"
	}
	commands := map[string]string{
		"command.git.status": "git status", "command.git.status.short": "git status --short",
		"command.git.diff": "git diff", "command.git.diff.stat": "git diff --stat",
		"command.git.log": "git log", "command.git.log.oneline": "git log --oneline -5",
		"command.git.show": "git show HEAD", "command.git.branch": "git branch",
		"command.git.branch.current": "git branch --show-current", "command.git.revparse": "git rev-parse HEAD",
		"command.rg.files": "rg --files", "command.ls": "ls", "command.ls.flags": "ls -al", "command.go.test": "go test ./...",
		"command.git.reset.hard": "git reset --hard",
	}
	for _, ruleID := range fixture.RuleIDs {
		if command := commands[ruleID]; command != "" {
			return command
		}
	}
	return "unregistered-fixture-command"
}

func shapeDigest(shape any) string {
	data, _ := json.Marshal(shape)
	return digestBytes(data)
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func safeFixtureName(value string) string {
	value = strings.ToLower(value)
	var result strings.Builder
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= '0' && char <= '9') || char == '-' || char == '_' || char == '.' {
			result.WriteRune(char)
		} else {
			result.WriteByte('-')
		}
	}
	return strings.Trim(result.String(), "-.")
}

func boolProperty(request authzen.EvaluationRequest, name string) bool {
	value, _ := request.Resource.Properties[name].(bool)
	return value
}

func readFixtureDirectory(directory string) ([]ClaudeFixture, []FixtureManifestEntry, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, nil, err
	}
	fixtures := []ClaudeFixture{}
	manifestEntries := []FixtureManifestEntry{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" || strings.Contains(strings.ToLower(entry.Name()), "manifest") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(directory, entry.Name()))
		if err != nil {
			return nil, nil, err
		}
		fixture, err := decodeFixture(data)
		if err != nil {
			return nil, nil, fmt.Errorf("fixture %s: %w", entry.Name(), err)
		}
		fixtures = append(fixtures, fixture)
		manifestEntries = append(manifestEntries, FixtureManifestEntry{File: entry.Name(), SHA256: digestBytes(data)})
	}
	sort.Slice(manifestEntries, func(i, j int) bool { return manifestEntries[i].File < manifestEntries[j].File })
	return fixtures, manifestEntries, nil
}

func decodeFixture(data []byte) (ClaudeFixture, error) {
	var fixture ClaudeFixture
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&fixture)
	return fixture, err
}

func readManifest(path string) (FixtureManifest, error) {
	var manifest FixtureManifest
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	err = decoder.Decode(&manifest)
	return manifest, err
}

func readVerifiedBundle(path, publicKeyPath string, now time.Time) (policybundle.Bundle, error) {
	var envelope policybundle.Envelope
	data, err := os.ReadFile(path)
	if err != nil {
		return policybundle.Bundle{}, err
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return policybundle.Bundle{}, err
	}
	publicKey, err := grants.LoadPublicKey(publicKeyPath)
	if err != nil {
		return policybundle.Bundle{}, fmt.Errorf("load policy bundle public key: %w", err)
	}
	bundle, err := policybundle.Verify(publicKey, envelope, now)
	if err != nil {
		return policybundle.Bundle{}, fmt.Errorf("verify active signed bundle: %w", err)
	}
	return bundle, nil
}

func verifyEquivalence(fixtures []ClaudeFixture, required []string) error {
	groups := map[string][]ClaudeFixture{}
	for _, fixture := range fixtures {
		groups[fixture.Scenario] = append(groups[fixture.Scenario], fixture)
	}
	for scenario, group := range groups {
		baseline := group[0]
		for _, fixture := range group[1:] {
			if fixture.Tool != baseline.Tool || fixture.InputSchemaDigest != baseline.InputSchemaDigest || fixture.Action != baseline.Action || fixture.ActualDecision != baseline.ActualDecision || fixture.ReasonCode != baseline.ReasonCode {
				return fmt.Errorf("scenario %s has non-equivalent model decisions or schemas", scenario)
			}
		}
		for _, family := range required {
			found := false
			for _, fixture := range group {
				if strings.Contains(strings.ToLower(fixture.Model), strings.ToLower(family)) {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("scenario %s is missing required model family %s", scenario, family)
			}
		}
	}
	return nil
}

func normalizedRequired(values []string) []string {
	set := map[string]bool{}
	for _, value := range values {
		if value = strings.ToLower(strings.TrimSpace(value)); value != "" {
			set[value] = true
		}
	}
	return sortedKeys(set)
}

func sortedKeys(values map[string]bool) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
