package lint

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SARIF 2.1.0 output.
//
// The point is CI, not another file format: GitHub's code-scanning upload takes SARIF and
// renders each result as an annotation on the line it refers to, inside the pull request.
// Without it a reviewer reads mekiki's findings in a job log and then goes looking for the
// file by hand.
//
// It pairs with `mekiki diff`, which emits only what the change added — that combination is
// the reviewable one, since an inherited corpus would otherwise annotate the backlog on
// every pull request.
//
// Written as plain structs rather than through a library: SARIF is a large specification but
// the useful subset is small, and a dependency here would contradict the binary's own
// supply-chain discipline. Only the fields GitHub actually consumes are emitted.

const sarifSchema = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json"

const sarifInfoURI = "https://github.com/toritori0318/mekiki"

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name           string           `json:"name"`
	Version        string           `json:"version"`
	InformationURI string           `json:"informationUri"`
	Rules          []sarifRuleDescr `json:"rules"`
}

// sarifRuleDescr carries the id alone. A short description per rule would duplicate the rule
// table in docs/DESIGN.md, and a copy that drifts is worse than an absent one — the finding
// message already says what is wrong.
type sarifRuleDescr struct {
	ID string `json:"id"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysical `json:"physicalLocation"`
}

type sarifPhysical struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           sarifRegion   `json:"region"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

// SARIF renders findings as a SARIF 2.1.0 log. version is the mekiki build version.
func SARIF(findings []Finding, version string) ([]byte, error) {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "" // fall back to absolute URIs rather than failing the run
	}

	results := make([]sarifResult, 0, len(findings))
	ruleSet := map[string]bool{}
	for _, f := range findings {
		ruleSet[f.Rule] = true
		line := f.Line
		if line < 1 {
			line = 1 // SARIF regions are 1-based; a corpus-level finding has no line
		}
		results = append(results, sarifResult{
			RuleID:  f.Rule,
			Level:   sarifLevel(f.Severity),
			Message: sarifMessage{Text: f.Message},
			Locations: []sarifLocation{{
				PhysicalLocation: sarifPhysical{
					ArtifactLocation: sarifArtifact{URI: sarifURI(f.File, cwd)},
					Region:           sarifRegion{StartLine: line},
				},
			}},
		})
	}

	ids := make([]string, 0, len(ruleSet))
	for id := range ruleSet {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	rules := make([]sarifRuleDescr, 0, len(ids))
	for _, id := range ids {
		rules = append(rules, sarifRuleDescr{ID: id})
	}

	log := sarifLog{
		Schema:  sarifSchema,
		Version: "2.1.0",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:           "mekiki",
				Version:        version,
				InformationURI: sarifInfoURI,
				Rules:          rules,
			}},
			Results: results,
		}},
	}
	raw, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

// sarifLevel maps mekiki severities onto the SARIF vocabulary, where the warning level is
// spelled in full.
func sarifLevel(sev string) string {
	if sev == Error {
		return "error"
	}
	return "warning"
}

// sarifURI makes a path usable by a consumer that resolves URIs against the repository root.
// A path outside the working directory is left absolute: a "../.." URI would resolve to
// nothing in a pull request view, and being visibly absolute is the more honest failure.
func sarifURI(path, cwd string) string {
	if cwd != "" {
		if rel, err := filepath.Rel(cwd, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(path)
}
