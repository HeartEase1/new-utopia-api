package model

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

const PaymentProviderAlipayF2F = "alipay_f2f"

var ErrAlipayAmountMismatch = errors.New("Alipay amount mismatch")

// RechargeAlipayF2F is shared by signed callbacks and authenticated queries.
// Amount matching, pending -> success and wallet credit are one transaction.
// Existing TopUp fields retain rc.49 semantics; no schema migration is needed.
func RechargeAlipayF2F(tradeNo string, paidAmount decimal.Decimal, callerIP string) error {
	if tradeNo == "" {
		return ErrTopUpNotFound
	}
	var topUp TopUp
	var quota int
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := lockForUpdate(tx).Where("trade_no = ?", tradeNo).First(&topUp).Error; err != nil {
			return err
		}
		if topUp.PaymentProvider != PaymentProviderAlipayF2F || topUp.PaymentMethod != PaymentProviderAlipayF2F {
			return ErrPaymentMethodMismatch
		}
		if !paidAmount.IsPositive() || !paidAmount.Equal(decimal.NewFromFloat(topUp.Money)) {
			return ErrAlipayAmountMismatch
		}
		if topUp.Status == common.TopUpStatusSuccess {
			return nil
		}
		if topUp.Status != common.TopUpStatusPending {
			return ErrTopUpStatusInvalid
		}
		credited, err := common.WalletQuotaFromDecimalStrict(decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit)))
		if err != nil || credited <= 0 {
			return ErrInvalidTopUpQuota
		}
		// The conditional write also protects against other order writers.
		result := tx.Model(&TopUp{}).Where("id = ? AND status = ?", topUp.Id, common.TopUpStatusPending).
			Updates(map[string]any{"status": common.TopUpStatusSuccess, "complete_time": common.GetTimestamp()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTopUpStatusInvalid
		}
		if err := creditTopUpQuota(tx, topUp.UserId, credited, nil); err != nil {
			return err
		}
		quota = credited
		return nil
	})
	if err != nil {
		return err
	}
	if quota > 0 {
		syncCreditUserQuotaCache(topUp.UserId, quota, "Alipay F2F top-up")
		RecordTopupLog(topUp.UserId, common.NewMessage("Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{
			"quota": logger.FormatQuota(quota), "amount": fmt.Sprintf("%.2f", topUp.Money),
		}), callerIP, PaymentProviderAlipayF2F, PaymentProviderAlipayF2F)
	}
	return nil
}
