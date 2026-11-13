package model

import (
	"errors"
	"math"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

// creditOnlineTopUpQuota runs only after a payment provider has verified payment
// and the caller has claimed the pending order within this same transaction.
// Manual credits, redemption and reward transfers use creditTopUpQuota directly.
func creditOnlineTopUpQuota(tx *gorm.DB, topUp *TopUp, quota int, updates map[string]any, cacheCredit *quotaCreditCache) error {
	if err := creditTopUpQuota(tx, topUp.UserId, quota, updates, cacheCredit); err != nil {
		return err
	}
	settings := operation_setting.GetReferralSetting()
	if !operation_setting.IsPaymentComplianceConfirmed() || settings.Percent <= 0 || settings.Percent > 100 || math.IsNaN(settings.Percent) || math.IsInf(settings.Percent, 0) || settings.Days < 1 || settings.Days > 3650 {
		return nil
	}
	var invited User
	if err := tx.Select("id", "inviter_id", "created_at").First(&invited, topUp.UserId).Error; err != nil {
		return err
	}
	now := common.GetTimestamp()
	// Missing legacy registration timestamps do not acquire a new eligibility window.
	if invited.InviterId <= 0 || invited.InviterId == invited.Id || invited.CreatedAt <= 0 || invited.CreatedAt > now || now-invited.CreatedAt >= int64(settings.Days)*86400 {
		return nil
	}
	var inviter User
	if err := tx.Select("id", "status").First(&inviter, invited.InviterId).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if inviter.Status != common.UserStatusEnabled {
		return nil
	}
	// Round down fractional wallet units, never round a rebate above its percentage.
	rebate, err := common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(int64(quota)).Mul(decimal.NewFromFloat(settings.Percent)).Div(decimal.NewFromInt(100)).Floor())
	if err != nil {
		return err
	}
	if rebate == 0 {
		return nil
	}
	result := tx.Model(&User{}).
		Where("id = ? AND status = ? AND aff_quota >= 0 AND aff_history >= 0 AND aff_quota <= ? AND aff_history <= ?", inviter.Id, common.UserStatusEnabled, common.MaxWalletQuota-rebate, common.MaxWalletQuota-rebate).
		Updates(map[string]any{"aff_quota": gorm.Expr("aff_quota + ?", rebate), "aff_history": gorm.Expr("aff_history + ?", rebate)})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		// Keep the order retryable instead of silently losing an earned rebate.
		return ErrWalletQuotaLimitExceeded
	}
	// This also provides a durable per-order record for reconciliation/refunds.
	return tx.Model(&TopUp{}).Where("id = ?", topUp.Id).
		Updates(map[string]any{"referral_user_id": inviter.Id, "referral_quota": rebate}).Error
}
