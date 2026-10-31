// Package result defines the review result contract.
package result

import (
	"reviewparty/internal/model"
)

const resultContractRevision = "canonical-v4"

// resultJSONSchema is the JSON Schema handed to harnesses that enforce
// structured output (Claude --json-schema, Codex --output-schema) and
// mirrored by the Go parser's semantic validation. Findings carry no ordinal:
// Review Party numbers them, so an agent cannot drift the sequence.
const resultJSONSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "type": "object",
  "additionalProperties": false,
  "required": ["status", "findings"],
  "properties": {
    "status": {"type": "string", "enum": ["clean", "findings"]},
    "caveats": {"type": "array", "maxItems": 4, "items": {"type": "string"}},
    "findings": {
      "type": "array",
      "maxItems": 8,
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["severity", "category", "location", "failure", "evidence", "fix", "test"],
        "properties": {
          "severity": {"type": "string", "enum": ["MEDIUM", "HIGH", "CRITICAL"]},
          "category": {"type": "string"},
          "location": {"type": "string"},
          "failure": {"type": "string"},
          "evidence": {"type": "string"},
          "fix": {"type": "string"},
          "test": {"type": "string"}
        }
      }
    }
  }
}`

type ResultContract struct{}

var CanonicalReviewResultContract ResultContract

func (ResultContract) Revision() string { return resultContractRevision }

// Schema returns the JSON Schema describing the reviewer's final message.
func (ResultContract) Schema() string { return resultJSONSchema }

func (ResultContract) Instructions() string {
	return `Return the review as one JSON object and nothing else: no prose before or after it and no markdown code fence.

A clean review:
{"status":"clean","caveats":[],"findings":[]}

A review with findings returns at most eight MEDIUM, HIGH, or CRITICAL findings:
{"status":"findings","caveats":[],"findings":[{"severity":"HIGH","category":"correctness","location":"path/to/file.go:123","failure":"Concrete supported scenario that fails.","evidence":"Why the Subject permits the failure.","fix":"Smallest safe correction.","test":"Regression that fails before the correction."}]}

"status" is "clean" with an empty findings array, or "findings" with one to eight findings. Every finding requires "severity", "category", "location", "failure", "evidence", "fix", and "test". "caveats" holds up to four short verification limits only, such as a claim you could not verify; anything actionable is a finding.`
}

func (ResultContract) Parse(assistantText string) (model.ReviewResult, error) {
	return parseReviewResult(assistantText)
}
