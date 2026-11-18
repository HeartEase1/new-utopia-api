package operation_setting

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestValidateWalletDisplayOption(t *testing.T) {
	for _, tc := range []struct {
		name, key, value string
		valid            bool
	}{
		{"empty notice", "payment_setting.wallet_announcement", "", true},
		{"long notice", "payment_setting.wallet_announcement", strings.Repeat("文", 2001), false},
		{"empty purchase", "payment_setting.redemption_purchase", "", true},
		{"clear link", "payment_setting.redemption_purchase", `{"name":"Store","url":""}`, true},
		{"store with fees", "payment_setting.redemption_purchase", `{"name":"Store","url":"https://store.example/codes?q=1","fee":"2%","payment_methods":"Alipay"}`, true},
		{"script URL", "payment_setting.redemption_purchase", `{"url":"javascript:alert(1)"}`, false},
		{"relative URL", "payment_setting.redemption_purchase", `{"url":"//store.example"}`, false},
		{"credential URL", "payment_setting.redemption_purchase", `{"url":"https://user:pass@store.example"}`, false},
		{"null config", "payment_setting.redemption_purchase", `null`, false},
		{"wrong type", "payment_setting.redemption_purchase", `{"fee":2}`, false},
		{"broken JSON", "payment_setting.redemption_purchase", `{`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateWalletDisplayOption(tc.key, tc.value)
			if tc.valid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
		})
	}
	oversized, err := common.Marshal(RedemptionPurchase{Name: strings.Repeat("x", 81)})
	require.NoError(t, err)
	require.Error(t, ValidateWalletDisplayOption("payment_setting.redemption_purchase", string(oversized)))
}
