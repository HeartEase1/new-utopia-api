package operation_setting

import (
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

// RedemptionStore describes an external store, not a payment or fee rule.
type RedemptionStore struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	Fee            string `json:"fee"`
	PaymentMethods string `json:"payment_methods"`
}

// RedemptionStoreList holds the stores shown in the wallet. Sites that saved
// the earlier single-store object keep loading it as a one-item list.
type RedemptionStoreList []RedemptionStore

func (stores *RedemptionStoreList) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		*stores = nil
		return nil
	}
	if trimmed[0] == '[' {
		var list []RedemptionStore
		if err := common.Unmarshal(data, &list); err != nil {
			return err
		}
		*stores = list
		return nil
	}
	var store RedemptionStore
	if err := common.Unmarshal(data, &store); err != nil {
		return err
	}
	*stores = RedemptionStoreList{store}
	return nil
}

const maxRedemptionStores = 20

func ValidateWalletDisplayOption(key, value string) error {
	switch key {
	case "payment_setting.wallet_announcement":
		if utf8.RuneCountInString(value) > 2000 {
			return common.NewMessage("Wallet announcement must not exceed 2000 characters")
		}
	case "payment_setting.redemption_purchase":
		if strings.TrimSpace(value) == "" {
			return nil
		}
		var stores RedemptionStoreList
		if len(value) > 16384 || common.UnmarshalJsonStr(value, &stores) != nil {
			return common.NewMessage("Invalid redemption purchase settings")
		}
		if len(stores) > maxRedemptionStores {
			return common.NewMessage("Invalid redemption purchase settings")
		}
		for _, store := range stores {
			if err := validateRedemptionStore(store); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateRedemptionStore(store RedemptionStore) error {
	if utf8.RuneCountInString(store.Name) > 80 ||
		utf8.RuneCountInString(store.Fee) > 200 ||
		utf8.RuneCountInString(store.PaymentMethods) > 200 ||
		len(store.URL) > 2048 {
		return common.NewMessage("Invalid redemption purchase settings")
	}
	if store.URL == "" {
		return nil
	}
	parsed, err := url.Parse(store.URL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil || strings.ContainsAny(store.URL, "\\\r\n\t ") {
		return common.NewMessage("Purchase link must be an HTTP or HTTPS URL without credentials")
	}
	return nil
}
