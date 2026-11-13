package operation_setting

import (
	"math"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/config"
)

type ReferralSetting struct {
	Percent float64 `json:"percent"`
	Days    int     `json:"days"`
}

var referralSetting = ReferralSetting{Days: 30}

func init() {
	config.GlobalConfig.Register("referral_setting", &referralSetting)
}

func GetReferralSetting() ReferralSetting {
	common.OptionMapRWMutex.RLock()
	defer common.OptionMapRWMutex.RUnlock()
	return referralSetting
}

func ValidateReferralOption(key, value string) error {
	switch key {
	case "referral_setting.percent":
		percent, err := strconv.ParseFloat(value, 64)
		if err != nil || math.IsNaN(percent) || math.IsInf(percent, 0) || percent < 0 || percent > 100 {
			return common.NewMessage("Referral percentage must be between 0 and 100")
		}
	case "referral_setting.days":
		days, err := strconv.Atoi(value)
		if err != nil || days < 1 || days > 3650 {
			return common.NewMessage("Referral period must be between 1 and 3650 whole days")
		}
	case "QuotaForInviter":
		if value != "0" {
			return common.NewMessage("Registration rewards for inviters have been replaced by top-up rebates")
		}
	}
	return nil
}
