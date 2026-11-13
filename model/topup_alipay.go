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
	BumpSyncTopic(SyncTopicOptions)
	return updateOptionMap(AlipayF2FOptionKey, value)
}

var ErrAlipayAmountMismatch = errors.New("Alipay amount mismatch")

// RechargeAlipayF2F is shared by signed callbacks and authenticated queries.
// Amount matching, pending -> success and wallet credit are one transaction.
// New orders use their checkout quota snapshot; legacy orders retain rc.49 semantics.
func RechargeAlipayF2F(tradeNo string, paidAmount decimal.Decimal, callerIP string) error {
	var cacheCredit quotaCreditCache
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
		// An expired order still settles: a verified payment of the exact amount
		// outranks a local judgement that the order had been abandoned.
		if topUp.Status != common.TopUpStatusPending && topUp.Status != common.TopUpStatusExpired {
			return ErrTopUpStatusInvalid
		}
		credited, err := topUp.alipayF2FQuota()
		if err != nil || credited <= 0 {
			return ErrInvalidTopUpQuota
		}
		// The conditional write also protects against other order writers.
		result := tx.Model(&TopUp{}).Where("id = ? AND status = ?", topUp.Id, topUp.Status).
			Updates(map[string]any{"status": common.TopUpStatusSuccess, "complete_time": common.GetTimestamp()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return ErrTopUpStatusInvalid
		}
		if err := creditOnlineTopUpQuota(tx, &topUp, credited, nil, &cacheCredit); err != nil {
			return err
		}
		quota = credited
		return nil
	})
	if err != nil {
		return err
	}
	if quota > 0 {
		syncCreditUserQuotaCache(topUp.UserId, quota, "Alipay F2F top-up", cacheCredit)
		RecordTopupLog(topUp.UserId, common.NewMessage("Online top-up succeeded, amount added: {{quota}}, payment amount: {{amount}}", map[string]any{
			"quota": logger.FormatQuota(quota), "amount": fmt.Sprintf("%.2f", topUp.Money),
		}), callerIP, PaymentProviderAlipayF2F, PaymentProviderAlipayF2F)
	}
	return nil
}

// alipayF2FQuota is shared with administrator completion so an order cannot
// receive a different credit by taking an alternative settlement path.
func (topUp *TopUp) alipayF2FQuota() (int, error) {
	quota := decimal.NewFromInt(topUp.QuotaSnapshot)
	if topUp.QuotaSnapshot == 0 {
		quota = decimal.NewFromInt(topUp.Amount).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	credited, err := common.WalletQuotaFromDecimalStrict(quota)
	if err != nil || credited <= 0 {
		return 0, ErrInvalidTopUpQuota
	}
	return credited, nil
}
