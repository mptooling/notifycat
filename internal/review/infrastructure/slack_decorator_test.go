package infrastructure

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockTypes returns the "type" of every raw block, in order.
func blockTypes(t *testing.T, blocks []json.RawMessage) []string {
	t.Helper()

	types := make([]string, len(blocks))
	for i, raw := range blocks {
		var probe struct {
			Type string `json:"type"`
		}
		require.NoError(t, json.Unmarshal(raw, &probe), "block %s", raw)
		types[i] = probe.Type
	}
	return types
}

func TestInsertBeforeActions_MarkerBeforeActionsBlock(t *testing.T) {
	blocks := []json.RawMessage{
		json.RawMessage(`{"type":"section"}`),
		json.RawMessage(`{"type":"actions"}`),
	}

	got := insertBeforeActions(blocks, json.RawMessage(`{"type":"context"}`))

	assert.Equal(t, []string{"section", "context", "actions"}, blockTypes(t, got))
}

func TestInsertBeforeActions_AppendsWhenNoActionsBlock(t *testing.T) {
	blocks := []json.RawMessage{json.RawMessage(`{"type":"section"}`)}

	got := insertBeforeActions(blocks, json.RawMessage(`{"type":"context"}`))

	assert.Equal(t, []string{"section", "context"}, blockTypes(t, got))
}

func TestSplitBlocks_MalformedOrEmptyYieldsNil(t *testing.T) {
	assert.Nil(t, splitBlocks(json.RawMessage(`not-json`)))
	assert.Nil(t, splitBlocks(nil))
}
