package infrastructure

import (
	"encoding/json"
	"testing"
)

func blockType(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatalf("unmarshal block %s: %v", raw, err)
	}
	return probe.Type
}

func TestInsertBeforeActions_MarkerBeforeActionsBlock(t *testing.T) {
	blocks := []json.RawMessage{
		json.RawMessage(`{"type":"section"}`),
		json.RawMessage(`{"type":"actions"}`),
	}
	marker := json.RawMessage(`{"type":"context"}`)
	out := insertBeforeActions(blocks, marker)
	if len(out) != 3 {
		t.Fatalf("got %d blocks; want 3", len(out))
	}
	if blockType(t, out[0]) != "section" || blockType(t, out[1]) != "context" || blockType(t, out[2]) != "actions" {
		t.Errorf("wrong order: %s, %s, %s; want section, context, actions", out[0], out[1], out[2])
	}
}

func TestInsertBeforeActions_AppendsWhenNoActionsBlock(t *testing.T) {
	blocks := []json.RawMessage{json.RawMessage(`{"type":"section"}`)}
	marker := json.RawMessage(`{"type":"context"}`)
	out := insertBeforeActions(blocks, marker)
	if len(out) != 2 || blockType(t, out[1]) != "context" {
		t.Errorf("marker not appended last: %v", out)
	}
}

func TestSplitBlocks_MalformedOrEmptyYieldsNil(t *testing.T) {
	if got := splitBlocks(json.RawMessage(`not-json`)); got != nil {
		t.Errorf("splitBlocks(malformed) = %v; want nil", got)
	}
	if got := splitBlocks(nil); got != nil {
		t.Errorf("splitBlocks(nil) = %v; want nil", got)
	}
}
