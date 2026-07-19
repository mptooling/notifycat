package application

import "testing"

// Golden strings pin the exact provider-facing JSON schemas byte-for-byte, so
// the refactor that builds the enum arrays from the domain constants cannot
// silently change the wire contract. If an enum constant changes, the built
// schema changes and this test flags the golden for a conscious update.
func TestDecisionSchemasAreByteStable(t *testing.T) {
	cases := []struct {
		name, got, want string
	}{
		{"open", string(openDecisionSchema()), wantOpenSchema},
		{"updated", string(updatedDecisionSchema()), wantUpdatedSchema},
		{"digest", string(digestDecisionSchema()), wantDigestSchema},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s schema drifted:\n got=%q\nwant=%q", tc.name, tc.got, tc.want)
		}
	}
}

const wantOpenSchema = `{
  "type": "object",
  "properties": {
    "targets": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "channel": {"type": "string"},
          "loudness": {"type": "string", "enum": ["ping", "quiet"]},
          "mentions": {"type": "array", "items": {"type": "string"}},
          "leading_emoji": {"type": "string"},
          "format": {"type": "string", "enum": ["standard", "compact"]},
          "emphasis": {"type": "string", "enum": ["none", "breaking"]},
          "context_block": {"type": "string"},
          "thread_note": {"type": "string"}
        },
        "required": ["channel", "loudness", "mentions", "leading_emoji", "format", "emphasis", "context_block", "thread_note"],
        "additionalProperties": false
      }
    },
    "rationale": {"type": "string"}
  },
  "required": ["targets", "rationale"],
  "additionalProperties": false
}`

const wantUpdatedSchema = `{
  "type": "object",
  "properties": {
    "emoji": {"type": "string"},
    "rationale": {"type": "string"}
  },
  "required": ["emoji", "rationale"],
  "additionalProperties": false
}`

const wantDigestSchema = `{
  "type": "object",
  "properties": {
    "order": {"type": "array", "items": {"type": "integer"}},
    "highlights": {"type": "array", "items": {"type": "string", "enum": ["normal", "attention"]}},
    "notes": {"type": "array", "items": {"type": "string"}},
    "parent_loudness": {"type": "string", "enum": ["ping", "quiet"]},
    "rationale": {"type": "string"}
  },
  "required": ["order", "highlights", "notes", "parent_loudness", "rationale"],
  "additionalProperties": false
}`
