package operation_setting

import (
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
)

// RedemptionPurchase describes an external store, not a payment or fee rule.
type RedemptionPurchase struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	Fee            string `json:"fee"`
	PaymentMethods string `json:"payment_methods"`
}

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
		var purchase *RedemptionPurchase
		if len(value) > 16384 || common.UnmarshalJsonStr(value, &purchase) != nil || purchase == nil {
			return common.NewMessage("Invalid redemption purchase settings")
		}
		if utf8.RuneCountInString(purchase.Name) > 80 || utf8.RuneCountInString(purchase.Fee) > 200 || utf8.RuneCountInString(purchase.PaymentMethods) > 200 || len(purchase.URL) > 2048 {
			return common.NewMessage("Invalid redemption purchase settings")
		}
		if purchase.URL != "" {
			parsed, err := url.Parse(purchase.URL)
			if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil || strings.ContainsAny(purchase.URL, "\\\r\n\t ") {
				return common.NewMessage("Purchase link must be an HTTP or HTTPS URL without credentials")
			}
		}
	}
	return nil
}
