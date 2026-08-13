package engine

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"reviewparty/internal/model"
)

const evalCaseSchemaVersion = 1

//go:embed evals
var packagedEvalFiles embed.FS

type evalSuiteManifest struct {
	SchemaVersion int      `json:"schema_version"`
	Name          string   `json:"name"`
	Revision      string   `json:"revision"`
	Cases         []string `json:"cases"`
}

type evalCaseFile struct {
	SchemaVersion    int                     `json:"schema_version"`
	ID               string                  `json:"id"`
	Mode             string                  `json:"mode"`
	Classification   string                  `json:"classification"`
	ExpectedFindings []model.ExpectedFinding `json:"expected_findings"`
	CleanEvidence    string                  `json:"clean_evidence,omitempty"`
	Base             string                  `json:"base,omitempty"`
	Head             string                  `json:"head"`
}

type preparedEvalCase struct {
	revision model.EvalCaseRevision
	base     string
	head     string
}

type loadedEvalSuite struct {
	name     string
	revision string
	digest   string
	cases    []preparedEvalCase
	cleanup  func()
}

type evalCaseLoad struct {
	source    fs.FS
	root      string
	casePath  string
	temporary string
	index     int
	packaged  bool
}

type evalSuiteMaterialization struct {
	source          fs.FS
	root            string
	temporary       string
	manifest        evalSuiteManifest
	manifestPayload []byte
	packaged        bool
}

func loadEvalSuite(reference string) (loadedEvalSuite, error) {
	switch reference {
	case "global:canary-bugs":
		return loadPackagedEvalSuite("evals/general-bugs")
	case "global:general-bugs":
		return loadPackagedEvalSuite("evals/realistic-general-bugs")
	}
	if reservedEvalSuiteName(reference) {
		return loadedEvalSuite{}, fmt.Errorf("unknown eval suite %q", reference)
	}
	absolute, err := filepath.Abs(reference)
	if err != nil {
		return loadedEvalSuite{}, err
	}
	return loadEvalSuiteFS(os.DirFS(absolute), ".")
}

func reservedEvalSuiteName(reference string) bool {
	return strings.HasPrefix(reference, "global:") || strings.HasPrefix(reference, "project:")
}

func loadEvalSuiteFS(source fs.FS, root string) (loadedEvalSuite, error) {
	return loadEvalSuiteSource(source, root, false)
}

func loadPackagedEvalSuite(root string) (loadedEvalSuite, error) {
	return loadEvalSuiteSource(packagedEvalFiles, root, true)
}

func loadEvalSuiteSource(source fs.FS, root string, packaged bool) (loadedEvalSuite, error) {
	manifest, payload, err := readEvalManifest(source, root)
	if err != nil {
		return loadedEvalSuite{}, err
	}
	temporary, err := os.MkdirTemp("", "review-party-eval-")
	if err != nil {
		return loadedEvalSuite{}, err
	}
	suite, err := materializeEvalCases(evalSuiteMaterialization{source: source, root: root, temporary: temporary, manifest: manifest, manifestPayload: payload, packaged: packaged})
	if err != nil {
		_ = os.RemoveAll(temporary)
		return loadedEvalSuite{}, err
	}
	suite.cleanup = func() { _ = os.RemoveAll(temporary) }
	return suite, nil
}

func readEvalManifest(source fs.FS, root string) (evalSuiteManifest, []byte, error) {
	payload, err := fs.ReadFile(source, filepath.ToSlash(filepath.Join(root, "suite.json")))
	if err != nil {
		return evalSuiteManifest{}, nil, fmt.Errorf("read eval suite: %w", err)
	}
	var manifest evalSuiteManifest
	if err := decodeStrict(payload, &manifest); err != nil {
		return evalSuiteManifest{}, nil, fmt.Errorf("decode eval suite: %w", err)
	}
	if err := validateEvalManifest(manifest); err != nil {
		return evalSuiteManifest{}, nil, err
	}
	return manifest, payload, nil
}

func validateEvalManifest(manifest evalSuiteManifest) error {
	if manifest.SchemaVersion != 1 {
		return errors.New("eval suite uses an unsupported schema version")
	}
	if manifest.Name == "" {
		return errors.New("eval suite requires a name")
	}
	if manifest.Revision == "" {
		return errors.New("eval suite requires a revision")
	}
	if len(manifest.Cases) == 0 {
		return errors.New("eval suite requires name, revision, and cases")
	}
	return nil
}

func materializeEvalCases(request evalSuiteMaterialization) (loadedEvalSuite, error) {
	hash := sha256.New()
	hash.Write(request.manifestPayload)
	seen := map[string]bool{}
	cases := make([]preparedEvalCase, 0, len(request.manifest.Cases))
	for index, casePath := range request.manifest.Cases {
		prepared, payloads, err := loadEvalCase(evalCaseLoad{source: request.source, root: request.root, casePath: casePath, temporary: request.temporary, index: index, packaged: request.packaged})
		if err != nil {
			return loadedEvalSuite{}, err
		}
		if seen[prepared.revision.ID] {
			return loadedEvalSuite{}, fmt.Errorf("duplicate eval case id %q", prepared.revision.ID)
		}
		seen[prepared.revision.ID] = true
		writeDigestPayloads(hash, payloads)
		cases = append(cases, prepared)
	}
	return loadedEvalSuite{name: request.manifest.Name, revision: request.manifest.Revision, digest: hex.EncodeToString(hash.Sum(nil)), cases: cases}, nil
}

func loadEvalCase(request evalCaseLoad) (preparedEvalCase, [][]byte, error) {
	caseFile := filepath.ToSlash(filepath.Join(request.root, request.casePath))
	payload, err := fs.ReadFile(request.source, caseFile)
	if err != nil {
		return preparedEvalCase{}, nil, err
	}
	definition, err := decodeEvalCase(request.casePath, payload)
	if err != nil {
		return preparedEvalCase{}, nil, err
	}
	base, head, err := createEvalCaseDestinations(request.temporary, request.index)
	if err != nil {
		return preparedEvalCase{}, nil, err
	}
	payloads, err := copyEvalCaseFixtures(evalFixtureCopy{source: request.source, caseRoot: filepath.Dir(caseFile), definition: definition, base: base, head: head, packaged: request.packaged})
	if err != nil {
		return preparedEvalCase{}, nil, err
	}
	payloads = append([][]byte{payload}, payloads...)
	revision := evalCaseRevision(definition, payloads)
	return preparedEvalCase{revision: revision, base: base, head: head}, payloads, nil
}

func decodeEvalCase(path string, payload []byte) (evalCaseFile, error) {
	var definition evalCaseFile
	if err := decodeStrict(payload, &definition); err != nil {
		return evalCaseFile{}, fmt.Errorf("decode eval case %q: %w", path, err)
	}
	if err := validateEvalCase(definition); err != nil {
		return evalCaseFile{}, fmt.Errorf("eval case %q: %w", definition.ID, err)
	}
	return definition, nil
}

func createEvalCaseDestinations(temporary string, index int) (string, string, error) {
	root := filepath.Join(temporary, fmt.Sprintf("%03d", index))
	base := filepath.Join(root, "base")
	head := filepath.Join(root, "head")
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(head, 0o700); err != nil {
		return "", "", err
	}
	return base, head, nil
}

type evalFixtureCopy struct {
	source     fs.FS
	caseRoot   string
	definition evalCaseFile
	base       string
	head       string
	packaged   bool
}

func copyEvalCaseFixtures(request evalFixtureCopy) ([][]byte, error) {
	var payloads [][]byte
	if request.definition.Mode == "change" {
		copied, err := copyFixtureDirectory(request.source, filepath.ToSlash(filepath.Join(request.caseRoot, request.definition.Base)), request.base, request.packaged)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, copied...)
	}
	copied, err := copyFixtureDirectory(request.source, filepath.ToSlash(filepath.Join(request.caseRoot, request.definition.Head)), request.head, request.packaged)
	if err != nil {
		return nil, err
	}
	return append(payloads, copied...), nil
}

func evalCaseRevision(definition evalCaseFile, payloads [][]byte) model.EvalCaseRevision {
	hash := sha256.New()
	writeDigestPayloads(hash, payloads)
	return model.EvalCaseRevision{ID: definition.ID, SchemaVersion: definition.SchemaVersion, Digest: hex.EncodeToString(hash.Sum(nil)), Mode: definition.Mode, Classification: definition.Classification, ExpectedFindings: definition.ExpectedFindings, CleanEvidence: definition.CleanEvidence}
}

func writeDigestPayloads(destination io.Writer, payloads [][]byte) {
	for _, payload := range payloads {
		_, _ = destination.Write(payload)
	}
}

func validateEvalCase(definition evalCaseFile) error {
	validators := []func(evalCaseFile) error{validateEvalCaseIdentity, validateEvalCaseFixture, validateEvalCaseClassification, validateExpectedFindings}
	for _, validate := range validators {
		if err := validate(definition); err != nil {
			return err
		}
	}
	return nil
}

func validateEvalCaseIdentity(definition evalCaseFile) error {
	if definition.SchemaVersion != evalCaseSchemaVersion {
		return errors.New("unsupported case schema version")
	}
	if definition.ID == "" {
		return errors.New("case id is required")
	}
	return nil
}

func validateEvalCaseFixture(definition evalCaseFile) error {
	if definition.Mode != "change" && definition.Mode != "state" {
		return errors.New("mode must be change or state")
	}
	if definition.Head == "" {
		return errors.New("head fixture is required")
	}
	if definition.Mode == "change" && definition.Base == "" {
		return errors.New("change case requires a base fixture")
	}
	return nil
}

func validateEvalCaseClassification(definition evalCaseFile) error {
	switch definition.Classification {
	case "defect":
		if len(definition.ExpectedFindings) == 0 || definition.CleanEvidence != "" {
			return errors.New("defect case requires expected Findings and no clean evidence")
		}
	case "known_clean":
		if len(definition.ExpectedFindings) != 0 || strings.TrimSpace(definition.CleanEvidence) == "" {
			return errors.New("known-clean case requires clean evidence and no expected Findings")
		}
	default:
		return errors.New("classification must be defect or known_clean")
	}
	return nil
}

func validateExpectedFindings(definition evalCaseFile) error {
	seen := map[string]bool{}
	for _, finding := range definition.ExpectedFindings {
		if finding.ID == "" {
			return errors.New("expected Finding id is required")
		}
		if finding.Behavior == "" {
			return errors.New("expected Finding behavior is required")
		}
		if finding.Impact == "" {
			return errors.New("expected Finding impact is required")
		}
		if len(finding.Evidence) == 0 {
			return errors.New("expected Finding requires id, behavior, impact, and evidence")
		}
		if seen[finding.ID] {
			return fmt.Errorf("duplicate expected Finding id %q", finding.ID)
		}
		seen[finding.ID] = true
	}
	return nil
}
