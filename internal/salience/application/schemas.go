package application

import "encoding/json"

// JSON Schemas enforced provider-side (Gemini responseJsonSchema / OpenAI
// json_schema response_format) and strict-parsed client-side regardless.

const openSchemaJSON = `{
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

const updatedSchemaJSON = `{
  "type": "object",
  "properties": {
    "emoji": {"type": "string"},
    "rationale": {"type": "string"}
  },
  "required": ["emoji", "rationale"],
  "additionalProperties": false
}`

const digestSchemaJSON = `{
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

func openDecisionSchema() json.RawMessage    { return json.RawMessage(openSchemaJSON) }
func updatedDecisionSchema() json.RawMessage { return json.RawMessage(updatedSchemaJSON) }
func digestDecisionSchema() json.RawMessage  { return json.RawMessage(digestSchemaJSON) }
