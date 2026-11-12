package model

import (
	"errors"
	"fmt"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	gormlogger "gorm.io/gorm/logger"
)

const PaymentProviderAlipayF2F = "alipay_f2f"
const AlipayF2FOptionKey = "AlipayF2FConfigSecret"

var alipayF2FOptionMutex sync.Mutex

// SaveAlipayF2FOption serializes concurrent settings writes, checks the
// administrator's snapshot, and protects pending orders when switching merchant.
func SaveAlipayF2FOption(expected, value string, merchantChanged bool) error {
	alipayF2FOptionMutex.Lock()
	defer alipayF2FOptionMutex.Unlock()
	// SQL errors must not interpolate the persisted private key into logs.
	err := DB.Session(&gorm.Session{Logger: gormlogger.Default.LogMode(gormlogger.Silent)}).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&Option{Key: AlipayF2FOptionKey}).Error; err != nil {
			return err
		}
		var current Option
		if err := lockForUpdate(tx).Where(&Option{Key: AlipayF2FOptionKey}).First(&current).Error; err != nil {
			return err
		}
		if current.Value != expected {
			return common.NewMessage("Alipay settings changed. Reload before saving.")
		}
		if merchantChanged {
			var pending int64
			if err := tx.Model(&TopUp{}).Where("payment_provider = ? AND status = ?", PaymentProviderAlipayF2F, common.TopUpStatusPending).Count(&pending).Error; err != nil {
				return err
			}
			if pending > 0 {
				return common.NewMessage("Settle pending Alipay orders before changing the merchant or environment.")
			}
		}
		return tx.Model(&current).Update("value", value).Error
	})
	if err != nil {
		return err
	}
	return updateOptionMap(AlipayF2FOptionKey, value)
}

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
