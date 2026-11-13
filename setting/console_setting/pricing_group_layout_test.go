package console_setting

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPricingGroupLayout(t *testing.T) {
	t.Run("valid layout retains order and collapsed preference", func(t *testing.T) {
		items, err := ParsePricingGroupLayout(`[{"id":"supplier","title":"Supplier","groups":["premium","free"],"default_open":false},{"id":"base","title":"","groups":["default"],"default_open":true}]`)
		require.NoError(t, err)
		require.Len(t, items, 2)
		assert.Equal(t, []string{"premium", "free"}, items[0].Groups)
		assert.False(t, items[0].DefaultOpen)
		assert.True(t, items[1].DefaultOpen)
	})
	for name, raw := range map[string]string{
		"invalid JSON":      `{`,
		"null":              `null`,
		"object":            `{}`,
		"null row":          `[null]`,
		"duplicate entry":   `[{"id":"a","title":"A","groups":[]},{"id":"a","title":"B","groups":[]}]`,
		"duplicate group":   `[{"id":"a","title":"A","groups":["default"]},{"id":"b","title":"B","groups":["default"]}]`,
		"missing members":   `[{"id":"a","title":"A"}]`,
		"unnamed drawer":    `[{"id":"a","title":"","groups":["one","two"]}]`,
		"blank drawer":      `[{"id":"a","title":"  ","groups":[]}]`,
		"blank group":       `[{"id":"a","title":"A","groups":[""]}]`,
		"wrong member type": `[{"id":"a","title":"A","groups":[1]}]`,
		"oversized payload": strings.Repeat(" ", 131073) + "[]",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ParsePricingGroupLayout(raw)
			require.Error(t, err)
		})
	}
	t.Run("filters private groups and empty drawers without changing membership order", func(t *testing.T) {
		raw := `[{"id":"private-key-name","title":"Mixed","groups":["hidden","free","premium"],"default_open":false},{"id":"secret","title":"Secret supplier","groups":["hidden2"]},{"id":"empty","title":"Empty","groups":[]},{"id":"auto","title":"Auto","groups":["auto","all"]}]`
		usable := map[string]string{"premium": "", "free": "", "auto": "", "all": ""}
		items := VisiblePricingGroupLayout(raw, usable)
		require.Len(t, items, 1)
		assert.Equal(t, []string{"free", "premium"}, items[0].Groups)
		assert.Equal(t, "Mixed", items[0].Title)
		assert.False(t, items[0].DefaultOpen)
		assert.Equal(t, items, VisiblePricingGroupLayout(raw, usable))
		encoded, err := common.Marshal(items)
		require.NoError(t, err)
		for _, secret := range []string{"hidden", "Secret supplier", "private-key-name"} {
			assert.NotContains(t, string(encoded), secret)
		}
		assert.Empty(t, VisiblePricingGroupLayout(raw, nil))
		assert.Empty(t, VisiblePricingGroupLayout("invalid", usable))
	})
}
