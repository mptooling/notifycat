package kernel_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mptooling/notifycat/internal/kernel"
)

func TestEventKindString(t *testing.T) {
	testCases := []struct {
		kind kernel.EventKind
		want string
	}{
		{kind: kernel.KindUpdated, want: "updated"},
		{kind: kernel.KindReopened, want: "reopened"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.want, func(t *testing.T) {
			assert.Equal(t, testCase.want, testCase.kind.String())
		})
	}
}
