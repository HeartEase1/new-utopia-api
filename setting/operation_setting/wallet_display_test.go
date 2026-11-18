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
		{"empty list", "payment_setting.redemption_purchase", `[]`, true},
		{"null config", "payment_setting.redemption_purchase", `null`, true},
		{"clear link", "payment_setting.redemption_purchase", `[{"name":"Store","url":""}]`, true},
		{"legacy single store", "payment_setting.redemption_purchase", `{"name":"Store","url":"https://store.example/codes","fee":"2%"}`, true},
		{"multiple stores", "payment_setting.redemption_purchase", `[{"name":"A","url":"https://a.example"},{"name":"B","url":"https://b.example/pay","fee":"1%","payment_methods":"Alipay"}]`, true},
		{"script URL", "payment_setting.redemption_purchase", `[{"url":"javascript:alert(1)"}]`, false},
		{"relative URL", "payment_setting.redemption_purchase", `[{"url":"//store.example"}]`, false},
		{"credential URL", "payment_setting.redemption_purchase", `[{"url":"https://user:pass@store.example"}]`, false},
		{"wrong type", "payment_setting.redemption_purchase", `[{"fee":2}]`, false},
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
	oversized, err := common.Marshal(RedemptionStoreList{{Name: strings.Repeat("x", 81)}})
	require.NoError(t, err)
	require.Error(t, ValidateWalletDisplayOption("payment_setting.redemption_purchase", string(oversized)))
	tooMany := make(RedemptionStoreList, maxRedemptionStores+1)
	for i := range tooMany {
		tooMany[i] = RedemptionStore{Name: "Store", URL: "https://store.example"}
	}
	encoded, err := common.Marshal(tooMany)
	require.NoError(t, err)
	require.Error(t, ValidateWalletDisplayOption("payment_setting.redemption_purchase", string(encoded)))
}

func TestRedemptionStoreListUnmarshalLegacyObject(t *testing.T) {
	var stores RedemptionStoreList
	require.NoError(t, common.UnmarshalJsonStr(`{"name":"Store","url":"https://store.example","payment_methods":"Alipay"}`, &stores))
	require.Equal(t, RedemptionStoreList{{Name: "Store", URL: "https://store.example", PaymentMethods: "Alipay"}}, stores)

	require.NoError(t, common.UnmarshalJsonStr(`[{"name":"A"},{"name":"B"}]`, &stores))
	require.Equal(t, RedemptionStoreList{{Name: "A"}, {Name: "B"}}, stores)
}
