package application

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mptooling/notifycat/internal/salience/domain"
)

// jsonEnum renders a JSON string-array literal (`["a", "b"]`) from enum values,
// matching the hand-written schema style so the schema stays byte-identical.
func jsonEnum(values ...string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = strconv.Quote(value)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// JSON Schemas enforced provider-side (Gemini responseJsonSchema / OpenAI
// json_schema response_format) and strict-parsed client-side regardless.

var openSchemaJSON = fmt.Sprintf(`{
  "type": "object",
  "properties": {
    "targets": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "channel": {"type": "string"},
          "loudness": {"type": "string", "enum": %s},
          "mentions": {"type": "array", "items": {"type": "string"}},
          "leading_emoji": {"type": "string"},
          "format": {"type": "string", "enum": %s},
          "emphasis": {"type": "string", "enum": %s},
          "context_block": {"type": "string"}
        },
        "required": ["channel", "loudness", "mentions", "leading_emoji", "format", "emphasis", "context_block"],
        "additionalProperties": false
      }
    },
    "rationale": {"type": "string"}
  },
  "required": ["targets", "rationale"],
  "additionalProperties": false
}`,
	jsonEnum(string(domain.LoudnessPing), string(domain.LoudnessQuiet)),
	jsonEnum(string(domain.FormatStandard), string(domain.FormatCompact)),
	jsonEnum(string(domain.EmphasisNone), string(domain.EmphasisBreaking)),
)

const updatedSchemaJSON = `{
  "type": "object",
  "properties": {
    "emoji": {"type": "string"},
    "rationale": {"type": "string"}
  },
  "required": ["emoji", "rationale"],
  "additionalProperties": false
}`

var digestSchemaJSON = fmt.Sprintf(`{
  "type": "object",
  "properties": {
    "order": {"type": "array", "items": {"type": "integer"}},
    "highlights": {"type": "array", "items": {"type": "string", "enum": %s}},
    "notes": {"type": "array", "items": {"type": "string"}},
    "parent_loudness": {"type": "string", "enum": %s},
    "rationale": {"type": "string"}
  },
  "required": ["order", "highlights", "notes", "parent_loudness", "rationale"],
  "additionalProperties": false
}`,
	jsonEnum(string(domain.HighlightNormal), string(domain.HighlightAttention)),
	jsonEnum(string(domain.LoudnessPing), string(domain.LoudnessQuiet)),
)

func openDecisionSchema() json.RawMessage    { return json.RawMessage(openSchemaJSON) }
func updatedDecisionSchema() json.RawMessage { return json.RawMessage(updatedSchemaJSON) }
func digestDecisionSchema() json.RawMessage  { return json.RawMessage(digestSchemaJSON) }
