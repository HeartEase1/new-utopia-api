package model

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/bytedance/gopkg/util/gopool"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	WalletTransferQuotePurpose       = "wallet_transfer_quote"
	WalletPasswordFlowPurpose        = "wallet_password"
	WalletTransferQuoteTTL           = 5 * time.Minute
	WalletPasswordFlowTTL            = 10 * time.Minute
	WalletPasswordMaxAttempts        = 5
	WalletTransferMaxAttempts        = 100
	WalletTransferDailyLimit         = 100
	WalletTransferStateRetentionDays = 90
)

var (
	ErrWalletTransferInvalid           = errors.New("Wallet transfer request is invalid.")
	ErrWalletTransferRecipientInvalid  = errors.New("The recipient is unavailable.")
	ErrWalletTransferInsufficient      = errors.New("Insufficient wallet balance.")
	ErrWalletTransferBalanceUnverified = errors.New("Your balance cannot be confirmed right now. Try again later.")
	ErrWalletTransferOutcomeUnknown    = errors.New("The transfer result could not be confirmed yet. Check the result before trying again.")
	ErrWalletTransferPreviousPending   = errors.New("A previous transfer is still being confirmed. Try again in a moment.")
	ErrWalletTransferBelowMinimum      = errors.New("The transfer amount is below the minimum of 1.")
	ErrWalletTransferDailyLimit        = errors.New("The daily wallet transfer limit has been reached.")
	ErrWalletTransferAttemptLimit      = errors.New("The daily wallet transfer attempt limit has been reached.")
	ErrWalletTransferPasswordRequired  = errors.New("Set a payment password before transferring balance.")
	ErrWalletTransferLoginRequired     = errors.New("Set a login password before transferring balance.")
	ErrWalletTransferPasswordInvalid   = errors.New("The login password or payment password is incorrect.")
	ErrWalletTransferLoginInvalid      = errors.New("The login password is incorrect.")
	ErrWalletTransferQuoteInvalid      = errors.New("This transfer review has expired or was already used. Review the transfer again.")
	ErrWalletTransferPasswordLocked    = errors.New("Payment password verification is temporarily locked.")
	ErrWalletTransferTwoFAInvalid      = errors.New("The two-factor authentication code is incorrect.")
	ErrWalletTransferTwoFALocked       = errors.New("Two-factor authentication is temporarily locked.")
	ErrWalletTransferTwoFAReplay       = errors.New("This two-factor authentication code has already been used.")
	ErrWalletPasswordFlowInvalid       = errors.New("The email verification flow is invalid or expired.")
	ErrWalletPasswordCodeInvalid       = errors.New("The email verification code is incorrect.")
	ErrWalletPasswordFlowLocked        = errors.New("Too many incorrect verification codes. Start again.")
	ErrWalletPasswordCurrentInvalid    = errors.New("The current payment password is incorrect.")
	ErrWalletPasswordSameAsLogin       = errors.New("The payment password must differ from the login password.")
	ErrWalletPasswordUnchanged         = errors.New("The new payment password must differ from the current payment password.")
	ErrWalletPasswordStateChanged      = errors.New("Payment password settings changed. Start again.")
	ErrWalletTransferPasswordState     = ErrWalletPasswordStateChanged
	ErrEmailBindingRequired            = errors.New("Bind an email address before managing the payment password.")
)

type WalletSecurity struct {
	Id                   int        `json:"-" gorm:"primaryKey"`
	UserId               int        `json:"-" gorm:"uniqueIndex;not null"`
	PasswordHash         string     `json:"-" gorm:"type:varchar(255);not null"`
	FailedAttempts       int        `json:"-" gorm:"not null;default:0"`
	LockedUntil          *time.Time `json:"-"`
	LastTransferTOTPStep int64      `json:"-" gorm:"not null;default:0"`
	CreatedAt            time.Time  `json:"-"`
	UpdatedAt            time.Time  `json:"-"`
}

type WalletTransferDailyConfig struct {
	Id              int64     `json:"-" gorm:"primaryKey"`
	UserId          int       `json:"-" gorm:"uniqueIndex:idx_wallet_transfer_day_user_date;not null"`
	DateKey         string    `json:"-" gorm:"type:varchar(10);uniqueIndex:idx_wallet_transfer_day_user_date;not null"`
	DisplayType     string    `json:"-" gorm:"type:varchar(16);not null"`
	DisplaySymbol   string    `json:"-" gorm:"type:varchar(16);not null"`
	DailyLimitQuota int64     `json:"-" gorm:"type:bigint;not null"`
	CreatedAt       time.Time `json:"-"`
}

type WalletTransferAttempt struct {
	Id          int64     `json:"-" gorm:"primaryKey"`
	QuoteFlowId int64     `json:"-" gorm:"uniqueIndex;not null"`
	UserId      int       `json:"-" gorm:"index:idx_wallet_transfer_attempt_user_day;not null"`
	DateKey     string    `json:"-" gorm:"type:varchar(10);index:idx_wallet_transfer_attempt_user_day;not null"`
	Quota       int64     `json:"-" gorm:"type:bigint;not null"`
	SubmittedAt time.Time `json:"-" gorm:"not null;index"`
}

type WalletTransfer struct {
	Id                   int64     `json:"id" gorm:"primaryKey"`
	QuoteFlowId          int64     `json:"-" gorm:"uniqueIndex;not null"`
	SenderId             int       `json:"sender_id" gorm:"index:idx_wallet_transfer_sender_date;not null"`
	SenderUsername       string    `json:"sender_username" gorm:"type:varchar(20);not null"`
	SenderDisplayName    string    `json:"sender_display_name" gorm:"type:varchar(20)"`
	RecipientId          int       `json:"recipient_id" gorm:"index:idx_wallet_transfer_recipient_date;not null"`
	RecipientUsername    string    `json:"recipient_username" gorm:"type:varchar(20);not null"`
	RecipientDisplayName string    `json:"recipient_display_name" gorm:"type:varchar(20)"`
	Amount               int64     `json:"amount" gorm:"type:bigint;not null"`
	Fee                  int64     `json:"fee" gorm:"type:bigint;not null"`
	Received             int64     `json:"received" gorm:"type:bigint;not null"`
	FeePercent           string    `json:"fee_percent" gorm:"type:varchar(16);not null"`
	DateKey              string    `json:"-" gorm:"type:varchar(10);index:idx_wallet_transfer_sender_date;index:idx_wallet_transfer_recipient_date;not null"`
	CreatedAt            time.Time `json:"created_at" gorm:"not null;index"`
	// TradeNo is derived from DateKey and Id, so it needs no column of its own.
	TradeNo string `json:"trade_no" gorm:"-"`
}

// WalletTransferTradeNo is the order number shown to users: "WT", the
// Asia/Shanghai date, and the record ID padded to at least eight digits.
func WalletTransferTradeNo(dateKey string, id int64) string {
	return fmt.Sprintf("WT%s%08d", strings.ReplaceAll(dateKey, "-", ""), id)
}

// ParseWalletTransferTradeNo returns the date key and record ID an order
// number stands for; ok is false for anything else.
func ParseWalletTransferTradeNo(tradeNo string) (dateKey string, id int64, ok bool) {
	digits, found := strings.CutPrefix(strings.ToUpper(strings.TrimSpace(tradeNo)), "WT")
	if !found || len(digits) < 16 || len(digits) > 26 {
		return "", 0, false
	}
	day, err := time.Parse("20060102", digits[:8])
	if err != nil {
		return "", 0, false
	}
	id, err = strconv.ParseInt(digits[8:], 10, 64)
	if err != nil || id <= 0 || WalletTransferTradeNo(day.Format("2006-01-02"), id) != "WT"+digits {
		return "", 0, false
	}
	return day.Format("2006-01-02"), id, true
}

func (t *WalletTransfer) AfterFind(*gorm.DB) error {
	t.TradeNo = WalletTransferTradeNo(t.DateKey, t.Id)
	return nil
}

func (t *WalletTransfer) AfterCreate(*gorm.DB) error {
	t.TradeNo = WalletTransferTradeNo(t.DateKey, t.Id)
	return nil
}

type WalletTransferQuote struct {
	RecipientId          int    `json:"recipient_id"`
	RecipientUsername    string `json:"recipient_username"`
	RecipientDisplayName string `json:"recipient_display_name"`
	Amount               int64  `json:"amount"`
	Fee                  int64  `json:"fee"`
	Received             int64  `json:"received"`
	FeePercent           string `json:"fee_percent"`
	DateKey              string `json:"-"`
	DailyLimitQuota      int64  `json:"daily_limit_quota"`
	DailyLimitAmount     int    `json:"daily_limit_amount"`
	DisplayType          string `json:"display_type"`
	DisplaySymbol        string `json:"display_symbol"`
	ExpiresAt            int64  `json:"expires_at"`
}

type WalletTransferQuoteResult struct {
	QuoteToken string              `json:"quote_token"`
	Quote      WalletTransferQuote `json:"quote"`
}

type WalletTransferStatus struct {
	HasLoginPassword bool `json:"has_login_password"`
	HasEmail         bool `json:"has_email"`
	EmailExempt      bool `json:"email_exempt"`
	// LimitsExempt: the root user has no daily amount cap or attempt limit.
	LimitsExempt       bool   `json:"limits_exempt"`
	Email              string `json:"email"`
	HasPaymentPassword bool   `json:"has_payment_password"`
	TwoFAEnabled       bool   `json:"two_fa_enabled"`
	AttemptCount       int64  `json:"attempt_count"`
	AttemptLimit       int64  `json:"attempt_limit"`
	FeePercent         string `json:"fee_percent"`
	UsedQuota          int64  `json:"used_quota"`
	DailyLimitQuota    int64  `json:"daily_limit_quota"`
	DailyLimitAmount   int    `json:"daily_limit_amount"`
	// MinQuota is the smallest amount one transfer may send.
	MinQuota      int64  `json:"min_quota"`
	DisplayType   string `json:"display_type"`
	DisplaySymbol string `json:"display_symbol"`
}

type WalletTransferDailyLimitSnapshot struct {
	Quota       int64  `json:"quota"`
	Amount      int    `json:"amount"`
	DisplayType string `json:"display_type"`
	Symbol      string `json:"symbol"`
}

type walletTransferQuoteState struct {
	SenderId             int    `json:"sender_id"`
	RecipientId          int    `json:"recipient_id"`
	RecipientUsername    string `json:"recipient_username"`
	RecipientDisplayName string `json:"recipient_display_name"`
	Amount               int64  `json:"amount"`
	Fee                  int64  `json:"fee"`
	Received             int64  `json:"received"`
	FeePercent           string `json:"fee_percent"`
	DateKey              string `json:"date_key"`
	DailyLimitQuota      int64  `json:"daily_limit_quota"`
	DailyLimitAmount     int    `json:"daily_limit_amount"`
	DisplayType          string `json:"display_type"`
	DisplaySymbol        string `json:"display_symbol"`
}

type walletPasswordFlowState struct {
	Intent   string `json:"intent"`
	Email    string `json:"email"`
	CodeHash string `json:"code_hash"`
	// EmailExempt marks a flow started by the root user without a bound email:
	// it carries no code, and stays valid only while both conditions hold.
	EmailExempt    bool `json:"email_exempt"`
	FailedAttempts int  `json:"failed_attempts"`
}

// The root user can already grant balance to anyone, so it manages its
// payment password without email verification, whether or not an email is
// bound. Every other check still applies.
func walletPasswordEmailExempt(user *User) bool {
	return user.Role == common.RoleRootUser
}

func walletTransferDateKey(now time.Time) string {
	location, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		location = time.FixedZone("Asia/Shanghai", 8*60*60)
	}
	return now.In(location).Format("2006-01-02")
}

func GetWalletTransferDailyLimitSnapshot() (*WalletTransferDailyLimitSnapshot, error) {
	displayType := operation_setting.GetQuotaDisplayType()
	rate := operation_setting.GetUsdToCurrencyRate(operation_setting.USDExchangeRate)
	symbol := operation_setting.GetCurrencySymbol()
	if displayType == operation_setting.QuotaDisplayTypeCustom {
		rate = operation_setting.GetGeneralSetting().CustomCurrencyExchangeRate
	}
	quota, err := checkedWalletTransferLimit(rate, displayType)
	if err != nil {
		return nil, err
	}
	if displayType == operation_setting.QuotaDisplayTypeTokens {
		symbol = "tokens"
	}
	return &WalletTransferDailyLimitSnapshot{Quota: quota, Amount: WalletTransferDailyLimit, DisplayType: displayType, Symbol: symbol}, nil
}

func walletTransferFee(amount int64, feePercent float64) (int64, string, error) {
	if amount <= 0 || amount > common.MaxWalletQuota || !operation_setting.ValidateWalletTransferFeePercent(feePercent) {
		return 0, "", ErrWalletTransferInvalid
	}
	rate := decimal.NewFromFloat(feePercent)
	feeDecimal := decimal.NewFromInt(amount).Mul(rate).Div(decimal.NewFromInt(100)).Ceil()
	fee, err := common.WalletQuotaFromDecimalStrict(feeDecimal)
	if err != nil || fee < 0 || int64(fee) >= amount {
		return 0, "", ErrWalletTransferInvalid
	}
	return int64(fee), rate.StringFixed(2), nil
}

func CreateWalletTransferQuote(identity AuthSessionIdentity, recipientId int, amount int64, feePercent float64, dailyLimitQuota int64, dailyLimitAmount int, displayType, displaySymbol string) (*WalletTransferQuoteResult, error) {
	if identity.UserID <= 0 || identity.SessionID == "" || recipientId <= 0 || amount <= 0 || amount > common.MaxWalletQuota || dailyLimitQuota <= 0 || dailyLimitQuota > common.MaxQuota || dailyLimitAmount != WalletTransferDailyLimit || len(displayType) > 16 || len(displaySymbol) > 16 {
		return nil, ErrWalletTransferInvalid
	}
	// The smallest transfer is one display unit: a hundredth of the daily cap.
	if amount < dailyLimitQuota/WalletTransferDailyLimit {
		return nil, ErrWalletTransferBelowMinimum
	}
	fee, feePercentSnapshot, err := walletTransferFee(amount, feePercent)
	if err != nil {
		return nil, err
	}
	state := walletTransferQuoteState{
		SenderId: identity.UserID, RecipientId: recipientId, Amount: amount, Fee: fee,
		Received: amount - fee, FeePercent: feePercentSnapshot, DateKey: walletTransferDateKey(time.Now()),
		DailyLimitQuota: dailyLimitQuota, DailyLimitAmount: dailyLimitAmount,
		DisplayType: displayType, DisplaySymbol: displaySymbol,
	}
	var result WalletTransferQuoteResult
	err = DB.Transaction(func(tx *gorm.DB) error {
		userIds := []int{identity.UserID, recipientId}
		sort.Ints(userIds)
		for _, userId := range userIds {
			var locked User
			if err := lockForUpdate(tx).Select("id").First(&locked, userId).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrWalletTransferRecipientInvalid
				}
				return err
			}
		}
		if err := ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		if recipientId == identity.UserID {
			return ErrWalletTransferRecipientInvalid
		}
		var recipient User
		if err := tx.Select("id", "username", "display_name", "status").First(&recipient, recipientId).Error; err != nil || recipient.Status != common.UserStatusEnabled {
			return ErrWalletTransferRecipientInvalid
		}
		var sender User
		if err := tx.Select("id").First(&sender, identity.UserID).Error; err != nil {
			return err
		}
		state.RecipientUsername = recipient.Username
		state.RecipientDisplayName = recipient.DisplayName
		config := WalletTransferDailyConfig{
			UserId: identity.UserID, DateKey: state.DateKey, DisplayType: displayType,
			DisplaySymbol: displaySymbol, DailyLimitQuota: dailyLimitQuota,
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&config).Error; err != nil {
			return err
		}
		var existing WalletTransferDailyConfig
		if err := tx.Where("user_id = ? AND date_key = ?", identity.UserID, state.DateKey).First(&existing).Error; err != nil {
			return err
		}
		state.DisplayType = existing.DisplayType
		state.DisplaySymbol = existing.DisplaySymbol
		state.DailyLimitQuota = existing.DailyLimitQuota
		payload, err := common.Marshal(state)
		if err != nil {
			return err
		}
		token, flow, err := createAuthFlowWithTx(tx, AuthFlowCreate{
			Purpose: WalletTransferQuotePurpose, UserId: identity.UserID, SessionId: identity.SessionID,
			Payload: string(payload), ExpiresAt: time.Now().Add(WalletTransferQuoteTTL),
		})
		if err != nil {
			return err
		}
		result = WalletTransferQuoteResult{
			QuoteToken: token,
			Quote: WalletTransferQuote{
				RecipientId: state.RecipientId, RecipientUsername: state.RecipientUsername,
				RecipientDisplayName: state.RecipientDisplayName, Amount: state.Amount, Fee: state.Fee,
				Received: state.Received, FeePercent: state.FeePercent, DailyLimitQuota: state.DailyLimitQuota,
				DailyLimitAmount: state.DailyLimitAmount, DisplayType: state.DisplayType,
				DisplaySymbol: state.DisplaySymbol, ExpiresAt: flow.ExpiresAt.Unix(),
			},
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func GetWalletTransferStatus(userId int, dailyLimitQuota int64, displayType, displaySymbol string) (*WalletTransferStatus, error) {
	if userId <= 0 || dailyLimitQuota <= 0 || dailyLimitQuota > common.MaxQuota {
		return nil, ErrWalletTransferInvalid
	}
	dateKey := walletTransferDateKey(time.Now())
	status := &WalletTransferStatus{
		MinQuota:         dailyLimitQuota / WalletTransferDailyLimit,
		DailyLimitQuota:  dailyLimitQuota,
		DailyLimitAmount: WalletTransferDailyLimit, DisplayType: displayType, DisplaySymbol: displaySymbol,
	}
	var day WalletTransferDailyConfig
	if err := DB.Where("user_id = ? AND date_key = ?", userId, dateKey).First(&day).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if day.UserId != 0 {
		status.DailyLimitQuota = day.DailyLimitQuota
		status.DisplayType = day.DisplayType
		status.DisplaySymbol = day.DisplaySymbol
	}
	var user User
	if err := DB.Select("password", "email", "role").First(&user, userId).Error; err != nil {
		return nil, err
	}
	status.EmailExempt = walletPasswordEmailExempt(&user)
	status.LimitsExempt = user.Role == common.RoleRootUser
	status.HasLoginPassword = user.Password != ""
	status.Email = common.MaskEmail(NormalizeEmail(user.Email))
	status.HasEmail = user.Email != ""
	var security WalletSecurity
	if err := DB.Select("user_id").Where("user_id = ?", userId).First(&security).Error; err == nil {
		status.HasPaymentPassword = true
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var twoFA TwoFA
	if err := DB.Select("is_enabled").Where("user_id = ? AND is_enabled = ?", userId, true).First(&twoFA).Error; err == nil {
		status.TwoFAEnabled = twoFA.IsEnabled
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if status.TwoFAEnabled {
		status.AttemptLimit = WalletTransferMaxAttempts
	} else {
		status.AttemptLimit = 10
	}
	if err := DB.Model(&WalletTransferAttempt{}).Where("user_id = ? AND date_key = ?", userId, dateKey).Count(&status.AttemptCount).Error; err != nil {
		return nil, err
	}
	if err := DB.Model(&WalletTransfer{}).Where("sender_id = ? AND date_key = ?", userId, dateKey).Select("COALESCE(SUM(amount), 0)").Scan(&status.UsedQuota).Error; err != nil {
		return nil, err
	}
	return status, nil
}

func CreateWalletTransferPasswordFlow(identity AuthSessionIdentity, intent, codeHash string) (string, time.Time, string, error) {
	if identity.UserID <= 0 || identity.SessionID == "" || codeHash == "" || (intent != "set" && intent != "change" && intent != "reset") {
		return "", time.Time{}, "", ErrWalletPasswordFlowInvalid
	}
	var token, email string
	expiresAt := time.Now().Add(WalletPasswordFlowTTL)
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		var user User
		if err := tx.Select("email", "role").First(&user, identity.UserID).Error; err != nil {
			return err
		}
		boundEmail := NormalizeEmail(user.Email)
		emailExempt := walletPasswordEmailExempt(&user)
		if boundEmail == "" && !emailExempt {
			return ErrEmailBindingRequired
		}
		if emailExempt {
			// No code is sent; the empty email tells the caller to skip delivery.
			codeHash = ""
		} else {
			email = boundEmail
		}
		var security WalletSecurity
		err := tx.Select("user_id").Where("user_id = ?", identity.UserID).First(&security).Error
		hasPassword := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if (intent == "set" && hasPassword) || ((intent == "change" || intent == "reset") && !hasPassword) {
			return ErrWalletTransferPasswordState
		}
		now := time.Now()
		if err := tx.Model(&AuthFlow{}).
			Where("purpose = ? AND user_id = ? AND consumed_at IS NULL", WalletPasswordFlowPurpose, identity.UserID).
			Update("consumed_at", now).Error; err != nil {
			return err
		}
		state, err := common.Marshal(walletPasswordFlowState{Intent: intent, Email: boundEmail, CodeHash: codeHash, EmailExempt: emailExempt})
		if err != nil {
			return err
		}
		token, _, err = createAuthFlowWithTx(tx, AuthFlowCreate{
			Purpose: WalletPasswordFlowPurpose, Intent: intent, UserId: identity.UserID,
			SessionId: identity.SessionID, Payload: string(state), ExpiresAt: expiresAt,
		})
		return err
	})
	return token, expiresAt, email, err
}

func CompleteWalletTransferPasswordFlow(identity AuthSessionIdentity, token, code, currentPassword, loginPassword, twoFACode, newPassword, newPasswordHash string) error {
	if identity.UserID <= 0 || identity.SessionID == "" || token == "" || newPasswordHash == "" || len(currentPassword) > common.MaxAccountPasswordLength*4 || len(loginPassword) > common.MaxAccountPasswordLength*4 || len(twoFACode) > 32 {
		return ErrWalletPasswordFlowInvalid
	}
	if err := common.ValidateNewAccountPassword(newPassword); err != nil {
		return err
	}
	if common.IsCommonPassword(newPassword) {
		return common.ErrPasswordTooCommon
	}
	var outcome error
	err := DB.Transaction(func(tx *gorm.DB) error {
		match := AuthFlowMatch{Purpose: WalletPasswordFlowPurpose, UserId: identity.UserID, SessionId: identity.SessionID}
		if err := applyAuthFlowMatch(tx.Model(&AuthFlow{}), token, match).
			Where("consumed_at IS NULL AND expires_at > ?", time.Now()).
			UpdateColumn("payload", gorm.Expr("payload")).Error; err != nil {
			return err
		}
		var flow AuthFlow
		if err := applyAuthFlowMatch(tx, token, match).First(&flow).Error; err != nil {
			return ErrWalletPasswordFlowInvalid
		}
		if flow.ConsumedAt != nil || !flow.ExpiresAt.After(time.Now()) {
			return ErrWalletPasswordFlowInvalid
		}
		var state walletPasswordFlowState
		if err := common.UnmarshalJsonStr(flow.Payload, &state); err != nil || (state.CodeHash == "" && !state.EmailExempt) || state.FailedAttempts >= WalletPasswordMaxAttempts {
			return ErrWalletPasswordFlowLocked
		}
		if err := ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		var user User
		if err := tx.Select("email", "password", "role").First(&user, identity.UserID).Error; err != nil {
			return err
		}
		if NormalizeEmail(user.Email) != state.Email || state.EmailExempt != walletPasswordEmailExempt(&user) {
			return ErrEmailBindingRequired
		}
		cleanCode, codeErr := common.ValidateNumericCode(code)
		if !state.EmailExempt && (codeErr != nil || !common.ValidatePasswordAndHash(cleanCode, state.CodeHash)) {
			state.FailedAttempts++
			payload, err := common.Marshal(state)
			if err != nil {
				return err
			}
			if err := tx.Model(&flow).Update("payload", string(payload)).Error; err != nil {
				return err
			}
			if state.FailedAttempts >= WalletPasswordMaxAttempts {
				outcome = ErrWalletPasswordFlowLocked
			} else {
				outcome = ErrWalletPasswordCodeInvalid
			}
			return nil
		}
		var security WalletSecurity
		securityErr := lockForUpdate(tx).Where("user_id = ?", identity.UserID).First(&security).Error
		hasSecurity := securityErr == nil
		if securityErr != nil && !errors.Is(securityErr, gorm.ErrRecordNotFound) {
			return securityErr
		}
		if hasSecurity != (state.Intent != "set") {
			return ErrWalletTransferPasswordState
		}
		now := time.Now()
		if hasSecurity && security.LockedUntil != nil {
			if now.Before(*security.LockedUntil) {
				return ErrWalletTransferPasswordLocked
			}
			security.FailedAttempts, security.LockedUntil = 0, nil
		}

		// The email code only proves control of the mailbox. A knowledge factor is
		// always required as well: the login password to set or reset, the current
		// payment password to change.
		var credentialErr error
		switch state.Intent {
		case "set", "reset":
			if user.Password == "" {
				return ErrWalletTransferLoginRequired
			}
			if !common.ValidatePasswordAndHash(loginPassword, user.Password) {
				credentialErr = ErrWalletTransferLoginInvalid
			}
		case "change":
			if !common.ValidatePasswordAndHash(currentPassword, security.PasswordHash) {
				credentialErr = ErrWalletPasswordCurrentInvalid
			}
		default:
			return ErrWalletPasswordFlowInvalid
		}
		if credentialErr != nil {
			state.FailedAttempts++
			payload, err := common.Marshal(state)
			if err != nil {
				return err
			}
			if err := tx.Model(&flow).Update("payload", string(payload)).Error; err != nil {
				return err
			}
			if hasSecurity {
				security.FailedAttempts++
				if security.FailedAttempts >= common.MaxFailAttempts {
					lockedUntil := now.Add(time.Duration(common.LockoutDuration) * time.Second)
					security.LockedUntil = &lockedUntil
				}
				if err := tx.Model(&security).Updates(map[string]any{"failed_attempts": security.FailedAttempts, "locked_until": security.LockedUntil}).Error; err != nil {
					return err
				}
			}
			outcome = credentialErr
			if state.FailedAttempts >= WalletPasswordMaxAttempts {
				outcome = ErrWalletPasswordFlowLocked
			}
			return nil
		}
		// Recovery must not bypass an enabled second factor.
		if state.Intent == "reset" {
			var twoFA TwoFA
			twoFAErr := lockForUpdate(tx).Where("user_id = ? AND is_enabled = ?", identity.UserID, true).First(&twoFA).Error
			if twoFAErr != nil && !errors.Is(twoFAErr, gorm.ErrRecordNotFound) {
				return twoFAErr
			}
			if twoFAErr == nil {
				if err := verifyWalletTransferTwoFactorWithTx(tx, &twoFA, &security, twoFACode, now); err != nil {
					outcome = err
					return nil
				}
			}
		}
		// Compared only after the caller is authenticated, so these answers cannot
		// be used to guess either password.
		if common.ValidatePasswordAndHash(newPassword, user.Password) {
			return ErrWalletPasswordSameAsLogin
		}
		if hasSecurity && common.ValidatePasswordAndHash(newPassword, security.PasswordHash) {
			return ErrWalletPasswordUnchanged
		}
		if !hasSecurity {
			if err := tx.Create(&WalletSecurity{UserId: identity.UserID, PasswordHash: newPasswordHash}).Error; err != nil {
				return err
			}
		} else if err := tx.Model(&security).Updates(map[string]any{"password_hash": newPasswordHash, "failed_attempts": 0, "locked_until": nil}).Error; err != nil {
			return err
		}
		return tx.Model(&flow).Update("consumed_at", time.Now()).Error
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrWalletPasswordFlowInvalid
		}
		return err
	}
	return outcome
}

// ValidateWalletTransferPasswordCode checks a short-lived flow before callers
// perform the memory-hard password hash. Invalid codes are committed as failed
// attempts, and completion rechecks the same code inside its atomic update.
func ValidateWalletTransferPasswordCode(identity AuthSessionIdentity, token, code string) error {
	if identity.UserID <= 0 || identity.SessionID == "" || token == "" {
		return ErrWalletPasswordFlowInvalid
	}
	var outcome error
	err := DB.Transaction(func(tx *gorm.DB) error {
		match := AuthFlowMatch{Purpose: WalletPasswordFlowPurpose, UserId: identity.UserID, SessionId: identity.SessionID}
		if err := applyAuthFlowMatch(tx.Model(&AuthFlow{}), token, match).
			Where("consumed_at IS NULL AND expires_at > ?", time.Now()).
			UpdateColumn("payload", gorm.Expr("payload")).Error; err != nil {
			return err
		}
		var flow AuthFlow
		if err := applyAuthFlowMatch(tx, token, match).First(&flow).Error; err != nil {
			return ErrWalletPasswordFlowInvalid
		}
		if flow.ConsumedAt != nil || !flow.ExpiresAt.After(time.Now()) {
			return ErrWalletPasswordFlowInvalid
		}
		var state walletPasswordFlowState
		if err := common.UnmarshalJsonStr(flow.Payload, &state); err != nil || (state.CodeHash == "" && !state.EmailExempt) || state.FailedAttempts >= WalletPasswordMaxAttempts {
			return ErrWalletPasswordFlowLocked
		}
		if err := ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		var user User
		if err := tx.Select("email", "role").First(&user, identity.UserID).Error; err != nil {
			return err
		}
		if NormalizeEmail(user.Email) != state.Email || state.EmailExempt != walletPasswordEmailExempt(&user) {
			return ErrEmailBindingRequired
		}
		if state.EmailExempt {
			return nil
		}
		numericCode, codeErr := common.ValidateNumericCode(code)
		if codeErr == nil && common.ValidatePasswordAndHash(numericCode, state.CodeHash) {
			return nil
		}
		state.FailedAttempts++
		payload, err := common.Marshal(state)
		if err != nil {
			return err
		}
		if err := tx.Model(&flow).Update("payload", string(payload)).Error; err != nil {
			return err
		}
		if state.FailedAttempts >= WalletPasswordMaxAttempts {
			outcome = ErrWalletPasswordFlowLocked
		} else {
			outcome = ErrWalletPasswordCodeInvalid
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrWalletPasswordFlowInvalid
		}
		return err
	}
	return outcome
}

type WalletTransferSubmit struct {
	QuoteToken      string
	LoginPassword   string
	PaymentPassword string
	TwoFACode       string
}

func ConfirmWalletTransfer(identity AuthSessionIdentity, input WalletTransferSubmit) (*WalletTransfer, error) {
	if identity.UserID <= 0 || identity.SessionID == "" || input.QuoteToken == "" || len(input.LoginPassword) > common.MaxAccountPasswordLength*4 || len(input.PaymentPassword) > common.MaxAccountPasswordLength*4 || len(input.TwoFACode) > 32 {
		return nil, ErrWalletTransferInvalid
	}
	// Hashing both passwords is the slow part. Do it before taking the lock
	// that also holds back batch quota flushes; the transaction reuses each
	// result only while the stored hash is still the one checked here.
	var checkedLoginHash, checkedPaymentHash string
	var checkedLoginValid, checkedPaymentValid bool
	var storedLogin User
	if DB.Select("password").First(&storedLogin, identity.UserID).Error == nil && storedLogin.Password != "" {
		checkedLoginHash = storedLogin.Password
		checkedLoginValid = common.ValidatePasswordAndHash(input.LoginPassword, checkedLoginHash)
	}
	var storedSecurity WalletSecurity
	if DB.Select("password_hash").Where("user_id = ?", identity.UserID).First(&storedSecurity).Error == nil && storedSecurity.PasswordHash != "" {
		checkedPaymentHash = storedSecurity.PasswordHash
		checkedPaymentValid = common.ValidatePasswordAndHash(input.PaymentPassword, checkedPaymentHash)
	}

	userQuotaBatchFlushMu.Lock()
	defer userQuotaBatchFlushMu.Unlock()

	// An undecided execution holds usage taken from the batch queue: it is in
	// the database already, or goes back to the queue. A new transfer must not
	// write a balance before that is known, so undecided executions are decided
	// first, under this lock, where no new one can appear; while one cannot be
	// decided, transfers wait.
	if !resolveAllUncertainWalletTransfers() {
		return nil, ErrWalletTransferPreviousPending
	}

	var transfer WalletTransfer
	var outcome error
	var pending map[int]int
	var cacheReserved bool
	// cacheOutcomeUnknown: a Redis error left it unclear whether the reservation ran.
	var cacheOutcomeUnknown bool
	// reservationKey names this execution's cache reservation. A retry with the
	// same review gets a new one, so undoing one execution never touches another.
	var reservationKey string
	var quoteFlowId int64
	var recipientCacheCredit quotaCreditCache
	var succeeded bool
	var alreadyCompleted bool
	err := DB.Transaction(func(tx *gorm.DB) error {
		match := AuthFlowMatch{Purpose: WalletTransferQuotePurpose, UserId: identity.UserID, SessionId: identity.SessionID}
		if err := applyAuthFlowMatch(tx.Model(&AuthFlow{}), input.QuoteToken, match).
			Where("consumed_at IS NULL AND expires_at > ?", time.Now()).
			UpdateColumn("payload", gorm.Expr("payload")).Error; err != nil {
			return err
		}
		var flow AuthFlow
		if err := applyAuthFlowMatch(tx, input.QuoteToken, match).First(&flow).Error; err != nil {
			return ErrWalletTransferQuoteInvalid
		}
		quoteFlowId = flow.Id
		// An earlier execution of this review whose commit answer was lost is
		// decided here: the flow row is locked (or already consumed), so its
		// state is final. Consumed means that execution committed.
		settleUncertainWalletTransfer(flow.Id, flow.ConsumedAt != nil)
		if flow.ConsumedAt != nil {
			if err := ValidateAuthSessionWithTx(tx, identity); err != nil {
				return err
			}
			if err := tx.Where("quote_flow_id = ?", flow.Id).First(&transfer).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrWalletTransferQuoteInvalid
				}
				return err
			}
			alreadyCompleted = true
			return nil
		}
		if !flow.ExpiresAt.After(time.Now()) {
			return ErrWalletTransferQuoteInvalid
		}
		var quote walletTransferQuoteState
		if err := common.UnmarshalJsonStr(flow.Payload, &quote); err != nil || quote.SenderId != identity.UserID || quote.RecipientId <= 0 || quote.Amount <= 0 || quote.Received <= 0 || quote.Fee < 0 || quote.Amount-quote.Fee != quote.Received {
			return ErrWalletTransferInvalid
		}
		now := time.Now()
		claim := applyAuthFlowMatch(tx.Model(&AuthFlow{}), input.QuoteToken, match).
			Where("consumed_at IS NULL AND expires_at > ?", now).
			Update("consumed_at", now)
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return ErrWalletTransferQuoteInvalid
		}
		userIds := []int{identity.UserID, quote.RecipientId}
		sort.Ints(userIds)
		for _, userId := range userIds {
			var locked User
			if err := lockForUpdate(tx).Select("id").First(&locked, userId).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return ErrWalletTransferRecipientInvalid
				}
				return err
			}
		}
		if err := ValidateAuthSessionWithTx(tx, identity); err != nil {
			return err
		}
		var sender User
		if err := tx.Select("id", "username", "display_name", "password", "status", "quota", "role").First(&sender, identity.UserID).Error; err != nil {
			return err
		}
		var recipient User
		if err := tx.Select("id", "username", "display_name", "status", "quota").First(&recipient, quote.RecipientId).Error; err != nil || recipient.Status != common.UserStatusEnabled || sender.Id == recipient.Id {
			return ErrWalletTransferRecipientInvalid
		}
		dateKey := walletTransferDateKey(now)
		if dateKey != quote.DateKey {
			return ErrWalletTransferQuoteInvalid
		}
		var twoFA TwoFA
		twoFAErr := lockForUpdate(tx).Where("user_id = ? AND is_enabled = ?", sender.Id, true).First(&twoFA).Error
		if twoFAErr != nil && !errors.Is(twoFAErr, gorm.ErrRecordNotFound) {
			return twoFAErr
		}
		attemptLimit := int64(10)
		if twoFAErr == nil {
			attemptLimit = WalletTransferMaxAttempts
		}
		var attempts int64
		if err := tx.Model(&WalletTransferAttempt{}).Where("user_id = ? AND date_key = ?", sender.Id, dateKey).Count(&attempts).Error; err != nil {
			return err
		}
		// The root user can already grant balance directly, so neither the
		// attempt limit nor the daily amount cap applies to it. Password and
		// 2FA lockouts still do.
		limitsExempt := sender.Role == common.RoleRootUser
		if !limitsExempt && attempts >= attemptLimit {
			outcome = ErrWalletTransferAttemptLimit
			return nil
		}
		attempt := WalletTransferAttempt{QuoteFlowId: flow.Id, UserId: sender.Id, DateKey: dateKey, Quota: quote.Amount, SubmittedAt: now}
		if err := tx.Create(&attempt).Error; err != nil {
			return err
		}
		if sender.Password == "" {
			outcome = ErrWalletTransferLoginRequired
			return nil
		}
		var security WalletSecurity
		if err := lockForUpdate(tx).Where("user_id = ?", sender.Id).First(&security).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				outcome = ErrWalletTransferPasswordRequired
				return nil
			}
			return err
		}
		if security.LockedUntil != nil {
			if now.Before(*security.LockedUntil) {
				outcome = ErrWalletTransferPasswordLocked
				return nil
			}
			security.FailedAttempts, security.LockedUntil = 0, nil
			if err := tx.Model(&security).Updates(map[string]any{"failed_attempts": 0, "locked_until": nil}).Error; err != nil {
				return err
			}
		}
		// Both are always checked and fail with one error, so a caller cannot
		// learn which of the two passwords was right.
		loginValid := checkedLoginValid
		if sender.Password != checkedLoginHash {
			loginValid = common.ValidatePasswordAndHash(input.LoginPassword, sender.Password)
		}
		paymentValid := checkedPaymentValid
		if security.PasswordHash != checkedPaymentHash {
			paymentValid = common.ValidatePasswordAndHash(input.PaymentPassword, security.PasswordHash)
		}
		if !loginValid || !paymentValid {
			security.FailedAttempts++
			if security.FailedAttempts >= common.MaxFailAttempts {
				lockedUntil := now.Add(time.Duration(common.LockoutDuration) * time.Second)
				security.LockedUntil = &lockedUntil
			}
			if err := tx.Model(&security).Updates(map[string]any{"failed_attempts": security.FailedAttempts, "locked_until": security.LockedUntil}).Error; err != nil {
				return err
			}
			outcome = ErrWalletTransferPasswordInvalid
			return nil
		}
		if security.FailedAttempts != 0 || security.LockedUntil != nil {
			if err := tx.Model(&security).Updates(map[string]any{"failed_attempts": 0, "locked_until": nil}).Error; err != nil {
				return err
			}
		}
		if twoFAErr == nil {
			if err := verifyWalletTransferTwoFactorWithTx(tx, &twoFA, &security, input.TwoFACode, now); err != nil {
				outcome = err
				return nil
			}
		}
		if twoFAErr != nil && !limitsExempt {
			var used int64
			if err := tx.Model(&WalletTransfer{}).Where("sender_id = ? AND date_key = ?", sender.Id, dateKey).Select("COALESCE(SUM(amount), 0)").Scan(&used).Error; err != nil {
				return err
			}
			if used > quote.DailyLimitQuota || quote.Amount > quote.DailyLimitQuota-used {
				outcome = ErrWalletTransferDailyLimit
				return nil
			}
		}
		if quote.RecipientId == sender.Id || quote.RecipientUsername != recipient.Username || quote.RecipientDisplayName != recipient.DisplayName || quote.Amount > common.MaxWalletQuota || quote.Received > common.MaxWalletQuota {
			return ErrWalletTransferRecipientInvalid
		}
		if common.RedisEnabled {
			reservationKey = walletTransferReservationKey(flow.Id)
			reserveResult, reserveErr := reserveWalletTransferQuota(sender.Id, reservationKey, quote.Amount)
			if reserveErr == nil && reserveResult == cacheQuotaMiss {
				if _, hydrateErr := GetUserCache(sender.Id); hydrateErr == nil {
					reserveResult, reserveErr = reserveWalletTransferQuota(sender.Id, reservationKey, quote.Amount)
				}
			}
			// With Redis, the cache is the authoritative available balance: it
			// holds reservations of running requests that the database may not
			// have received yet. Money only moves once the cache has reserved
			// the amount; when it refuses, fails or cannot be loaded, the
			// database balance may be stale, so the transfer is refused.
			if reserveErr != nil || reserveResult != cacheQuotaOK {
				outcome = ErrWalletTransferBalanceUnverified
				if reserveErr == nil && reserveResult == cacheQuotaInsufficient {
					outcome = ErrWalletTransferInsufficient
				}
				cacheOutcomeUnknown = reserveErr != nil
				return nil
			}
			cacheReserved = true
		}
		pending = takeBatchUserQuotaDeltas(sender.Id, recipient.Id)
		senderEffective := int64(sender.Quota) + int64(pending[sender.Id])
		recipientEffective := int64(recipient.Quota) + int64(pending[recipient.Id])
		if senderEffective < quote.Amount {
			outcome = ErrWalletTransferInsufficient
			return nil
		}
		// Wallet balances are bounded by MaxWalletQuota, not by the per-request
		// MaxQuota.
		if recipientEffective < 0 || recipientEffective > common.MaxWalletQuota-quote.Received {
			return ErrWalletQuotaLimitExceeded
		}
		newSenderQuota := senderEffective - quote.Amount
		newRecipientQuota := recipientEffective + quote.Received
		if newSenderQuota < 0 || newSenderQuota > common.MaxWalletQuota || newRecipientQuota > common.MaxWalletQuota {
			return ErrWalletQuotaLimitExceeded
		}
		if err := tx.Model(&User{}).Where("id = ?", sender.Id).Update("quota", newSenderQuota).Error; err != nil {
			return err
		}
		if common.RedisEnabled {
			recipientCacheCredit.generation, _ = common.RDB.HGet(context.Background(), getUserCacheKey(recipient.Id), "QuotaGeneration").Result()
		}
		if err := tx.Model(&User{}).Where("id = ?", recipient.Id).Update("quota", newRecipientQuota).Error; err != nil {
			return err
		}
		transfer = WalletTransfer{
			QuoteFlowId: flow.Id, SenderId: sender.Id, SenderUsername: sender.Username,
			SenderDisplayName: sender.DisplayName, RecipientId: recipient.Id,
			RecipientUsername: recipient.Username, RecipientDisplayName: recipient.DisplayName,
			Amount: quote.Amount, Fee: quote.Fee, Received: quote.Received, FeePercent: quote.FeePercent,
			DateKey: dateKey, CreatedAt: now,
		}
		if err := tx.Create(&transfer).Error; err != nil {
			return err
		}
		succeeded = true
		return nil
	})
	// The work ran but the commit reported an error, so it may or may not
	// have been committed. Restoring the queued usage would charge it twice if
	// it was; dropping it would lose it if it was not. Park both the usage and
	// the reservation until the database can tell.
	if succeeded && err != nil {
		reservation := ""
		if cacheReserved {
			reservation = reservationKey
		}
		common.SysError(common.LogText("wallet transfer commit outcome unknown for quote flow %d: %s", quoteFlowId, err.Error()))
		parkUncertainWalletTransfer(&uncertainWalletTransfer{
			flowId: quoteFlowId, senderId: identity.UserID, pending: pending, reservationKey: reservation,
		})
		return nil, ErrWalletTransferOutcomeUnknown
	}
	committed := succeeded && err == nil
	if len(pending) > 0 && !committed {
		restoreBatchUserQuotaDeltas(pending)
	}
	switch {
	case committed && cacheReserved:
		forgetWalletTransferReservation(reservationKey)
	case !committed && (cacheReserved || cacheOutcomeUnknown):
		// Undo this execution's reservation only. Dropping the whole cache would
		// also drop reservations of running requests and let a retry spend them.
		releaseWalletTransferReservation(identity.UserID, reservationKey)
	}
	if err != nil {
		return nil, err
	}
	if alreadyCompleted {
		return &transfer, nil
	}
	if outcome != nil {
		return nil, outcome
	}
	RecordLog(transfer.SenderId, LogTypeSystem, common.NewMessage("Transferred {{quota}} to {{username}} (ID {{id}}), fee {{fee}}, order {{trade_no}}", map[string]any{
		"quota": logger.FormatQuota(int(transfer.Amount)), "username": transfer.RecipientUsername,
		"id": transfer.RecipientId, "fee": logger.FormatQuota(int(transfer.Fee)), "trade_no": transfer.TradeNo,
	}))
	RecordLog(transfer.RecipientId, LogTypeSystem, common.NewMessage("Received {{quota}} from {{username}} (ID {{id}}), order {{trade_no}}", map[string]any{
		"quota": logger.FormatQuota(int(transfer.Received)), "username": transfer.SenderUsername,
		"id": transfer.SenderId, "trade_no": transfer.TradeNo,
	}))
	if common.RedisEnabled {
		// The sender's cache already holds this transfer's reservation; never
		// drop it, as that would also drop reservations of running requests.
		syncCreditUserQuotaCache(transfer.RecipientId, int(transfer.Received), "wallet transfer", recipientCacheCredit)
	}
	return &transfer, nil
}

const walletTransferReservationTTL = 10 * time.Minute

// walletTransferReserveScript takes the amount from the cached balance and, in
// the same step, records what it took under this execution's key, with the
// cache generation it applied to. The Redis client may resend the command when
// an answer is lost; with the same key it then takes nothing a second time.
// Each execution has its own key, so this never lets one execution reuse
// another's reservation.
const walletTransferReserveScript = `
if redis.call('EXISTS', KEYS[2]) == 1 then
  return 1
end
if tonumber(redis.call('HGET', KEYS[1], 'Id') or '0') ~= tonumber(ARGV[2])
  or tonumber(redis.call('HGET', KEYS[1], 'CacheSchema') or '0') ~= tonumber(ARGV[3])
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return -1
end
local quota = tonumber(redis.call('HGET', KEYS[1], 'Quota'))
if quota == nil or quota < tonumber(ARGV[1]) then
  return 0
end
redis.call('HINCRBY', KEYS[1], 'Quota', -tonumber(ARGV[1]))
redis.call('HSET', KEYS[2], 'amount', ARGV[1], 'generation', redis.call('HGET', KEYS[1], 'QuotaGeneration') or '')
redis.call('EXPIRE', KEYS[2], ARGV[4])
return 1`

// walletTransferReleaseScript gives back exactly what the reservation took, if
// it was taken and the cache it applied to is still the current one.
const walletTransferReleaseScript = `
if redis.call('EXISTS', KEYS[2]) == 0 then
  return 0
end
local amount = redis.call('HGET', KEYS[2], 'amount')
local generation = redis.call('HGET', KEYS[2], 'generation') or ''
redis.call('DEL', KEYS[2])
if (redis.call('HGET', KEYS[1], 'QuotaGeneration') or '') ~= generation
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return 0
end
redis.call('HINCRBY', KEYS[1], 'Quota', amount)
return 1`

// walletTransferReservationKey is unique per execution: the review's flow ID
// plus a random suffix, so retries of one review never share a reservation.
func walletTransferReservationKey(flowId int64) string {
	return fmt.Sprintf("wallet_transfer:reservation:%d:%s", flowId, common.GetRandomString(16))
}

func reserveWalletTransferQuota(userId int, key string, amount int64) (cacheQuotaResult, error) {
	result, err := common.RDB.Eval(context.Background(), walletTransferReserveScript,
		[]string{getUserCacheKey(userId), key},
		amount, userId, userCacheSchemaVersion, int(walletTransferReservationTTL/time.Second)).Int()
	return quotaResultFromLua(result, err)
}

// releaseWalletTransferReservation undoes a reservation whose transfer did not
// happen. Releasing one that was never taken does nothing, so it is safe after
// an unclear Redis answer. While Redis stays unreachable it retries in the
// background; if it never succeeds the cache only under-reports the balance
// until it is reloaded, which can never let money be spent twice.
func releaseWalletTransferReservation(userId int, key string) {
	// The background retries keep using the client this reservation was made on.
	client := common.RDB
	release := func() error {
		return client.Eval(context.Background(), walletTransferReleaseScript,
			[]string{getUserCacheKey(userId), key}).Err()
	}
	if err := release(); err == nil {
		return
	}
	gopool.Go(func() {
		for attempt := 1; attempt <= 5; attempt++ {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
			err := release()
			if err == nil {
				return
			}
			if attempt == 5 {
				common.SysError(common.LogText("failed to release wallet transfer reservation %s for user %d: %s", key, userId, err.Error()))
			}
		}
	})
}

// forgetWalletTransferReservation drops the record of a reservation whose
// transfer was committed; it expires on its own if this fails.
func forgetWalletTransferReservation(key string) {
	_ = common.RDB.Del(context.Background(), key).Err()
}

// uncertainWalletTransfer is an execution whose commit reported an error. Its
// queued usage and cache reservation wait here until it is known whether the
// commit happened.
type uncertainWalletTransfer struct {
	flowId         int64
	senderId       int
	pending        map[int]int
	reservationKey string
}

// uncertainWalletTransfers holds parked executions by quote flow ID.
var uncertainWalletTransfers sync.Map

// walletTransferResolveDelay is the wait before each background check of a
// parked execution. The first wait lets an in-flight commit finish.
var walletTransferResolveDelay = func(attempt int) time.Duration {
	return min(time.Duration(attempt+1)*5*time.Second, 10*time.Minute)
}

func parkUncertainWalletTransfer(item *uncertainWalletTransfer) {
	uncertainWalletTransfers.Store(item.flowId, item)
	gopool.Go(func() {
		for attempt := 0; ; attempt++ {
			time.Sleep(walletTransferResolveDelay(attempt))
			if _, parked := uncertainWalletTransfers.Load(item.flowId); !parked {
				return
			}
			err := resolveUncertainWalletTransfer(item.flowId)
			if err == nil {
				return
			}
			if attempt%10 == 9 {
				common.SysError(common.LogText("wallet transfer for quote flow %d is still undecided: %s", item.flowId, err.Error()))
			}
		}
	})
}

// resolveUncertainWalletTransfer decides a parked execution from the database.
// It locks the quote flow row, which waits for a commit still in flight; a
// consumed flow means the execution committed. It returns an error, and keeps
// the execution parked, while the database cannot be read.
func resolveUncertainWalletTransfer(flowId int64) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var flow AuthFlow
		err := lockForUpdate(tx).Select("id", "consumed_at").First(&flow, flowId).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// The flow was cleaned up; the transfer row tells instead.
			var transfers int64
			if err := tx.Model(&WalletTransfer{}).Where("quote_flow_id = ?", flowId).Count(&transfers).Error; err != nil {
				return err
			}
			settleUncertainWalletTransfer(flowId, transfers > 0)
			return nil
		}
		if err != nil {
			return err
		}
		settleUncertainWalletTransfer(flowId, flow.ConsumedAt != nil)
		return nil
	})
}

// settleUncertainWalletTransfer applies the decision for a parked execution,
// once. If it committed, its usage is already in the balance and its
// reservation stays taken; otherwise the usage goes back to the queue and the
// reservation is returned.
func settleUncertainWalletTransfer(flowId int64, committed bool) {
	value, parked := uncertainWalletTransfers.LoadAndDelete(flowId)
	if !parked {
		return
	}
	item := value.(*uncertainWalletTransfer)
	if committed {
		if item.reservationKey != "" {
			forgetWalletTransferReservation(item.reservationKey)
		}
		return
	}
	restoreBatchUserQuotaDeltas(item.pending)
	if item.reservationKey != "" {
		releaseWalletTransferReservation(item.senderId, item.reservationKey)
	}
}

// resolveAllUncertainWalletTransfers tries to decide every parked execution
// and reports whether none is left.
func resolveAllUncertainWalletTransfers() bool {
	settled := true
	uncertainWalletTransfers.Range(func(key, _ any) bool {
		if err := resolveUncertainWalletTransfer(key.(int64)); err != nil {
			settled = false
		}
		return true
	})
	return settled
}

// SettlePendingQuotaBeforeShutdown decides undecided wallet transfers, which
// returns usage of rolled-back ones to the batch queue, then writes the queue.
// Both live only in memory, so a graceful restart would otherwise lose them.
func SettlePendingQuotaBeforeShutdown() {
	if !resolveAllUncertainWalletTransfers() {
		uncertainWalletTransfers.Range(func(key, value any) bool {
			item := value.(*uncertainWalletTransfer)
			common.SysError(common.LogText("wallet transfer for quote flow %v is still undecided at shutdown; queued quota changes held with it: %v", key, item.pending))
			return true
		})
	}
	if common.BatchUpdateEnabled {
		batchUpdate()
	}
}

func verifyWalletTransferTwoFactorWithTx(tx *gorm.DB, twoFA *TwoFA, security *WalletSecurity, code string, now time.Time) error {
	if twoFA == nil || !twoFA.IsEnabled {
		return ErrWalletTransferTwoFAInvalid
	}
	if twoFA.IsLocked() {
		return ErrWalletTransferTwoFALocked
	}
	code = strings.TrimSpace(code)
	numericCode, numericErr := common.ValidateNumericCode(code)
	if numericErr == nil {
		step, valid := matchingWalletTransferTOTPStep(twoFA.Secret, numericCode, now)
		if !valid {
			if err := incrementWalletTransferTwoFAFailure(tx, twoFA, now); err != nil {
				return err
			}
			return ErrWalletTransferTwoFAInvalid
		}
		if step <= security.LastTransferTOTPStep || (twoFA.LastUsedAt != nil && step <= twoFA.LastUsedAt.Unix()/30) {
			return ErrWalletTransferTwoFAReplay
		}
		security.LastTransferTOTPStep = step
		twoFA.FailedAttempts, twoFA.LockedUntil = 0, nil
		if err := tx.Model(security).Update("last_transfer_totp_step", step).Error; err != nil {
			return err
		}
		return tx.Model(twoFA).Updates(map[string]any{"failed_attempts": 0, "locked_until": nil, "last_used_at": now}).Error
	}
	if common.ValidateBackupCode(code) {
		var backupCodes []TwoFABackupCode
		if err := tx.Where("user_id = ? AND is_used = ?", twoFA.UserId, false).Find(&backupCodes).Error; err != nil {
			return err
		}
		for _, backup := range backupCodes {
			normalized := common.NormalizeBackupCode(code)
			if !common.ValidatePasswordAndHash(normalized, backup.CodeHash) {
				continue
			}
			result := tx.Model(&TwoFABackupCode{}).Where("id = ? AND is_used = ?", backup.Id, false).Updates(map[string]any{"is_used": true, "used_at": now})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrWalletTransferTwoFAInvalid
			}
			twoFA.FailedAttempts, twoFA.LockedUntil = 0, nil
			return tx.Model(twoFA).Updates(map[string]any{"failed_attempts": 0, "locked_until": nil, "last_used_at": now}).Error
		}
	}
	if err := incrementWalletTransferTwoFAFailure(tx, twoFA, now); err != nil {
		return err
	}
	return ErrWalletTransferTwoFAInvalid
}

func matchingWalletTransferTOTPStep(secret, code string, now time.Time) (int64, bool) {
	options := totp.ValidateOpts{Period: 30, Skew: 1, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1}
	currentStep := now.Unix() / int64(options.Period)
	for _, step := range []int64{currentStep, currentStep - 1, currentStep + 1} {
		candidateTime := time.Unix(step*int64(options.Period), 0)
		candidate, err := totp.GenerateCodeCustom(secret, candidateTime, options)
		if err == nil && subtle.ConstantTimeCompare([]byte(candidate), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

func incrementWalletTransferTwoFAFailure(tx *gorm.DB, twoFA *TwoFA, now time.Time) error {
	attempts := twoFA.FailedAttempts + 1
	if twoFA.LockedUntil != nil && !now.Before(*twoFA.LockedUntil) {
		attempts = 1
	}
	var lockedUntil *time.Time
	if attempts >= common.MaxFailAttempts {
		value := now.Add(time.Duration(common.LockoutDuration) * time.Second)
		lockedUntil = &value
	}
	if err := tx.Model(twoFA).Updates(map[string]any{"failed_attempts": attempts, "locked_until": lockedUntil}).Error; err != nil {
		return err
	}
	twoFA.FailedAttempts, twoFA.LockedUntil = attempts, lockedUntil
	return nil
}

func takeBatchUserQuotaDeltas(userIds ...int) map[int]int {
	batchUpdateLocks[BatchUpdateTypeUserQuota].Lock()
	defer batchUpdateLocks[BatchUpdateTypeUserQuota].Unlock()
	pending := make(map[int]int, len(userIds))
	for _, userId := range userIds {
		pending[userId] = batchUpdateStores[BatchUpdateTypeUserQuota][userId]
		delete(batchUpdateStores[BatchUpdateTypeUserQuota], userId)
	}
	return pending
}

func restoreBatchUserQuotaDeltas(pending map[int]int) {
	for userId, delta := range pending {
		if delta != 0 {
			addNewRecord(BatchUpdateTypeUserQuota, userId, delta)
		}
	}
}

// SearchWalletTransferRecipients finds a recipient by complete username or user
// ID only. Partial matches are never returned, so the search cannot be used to
// browse other users.
func SearchWalletTransferRecipients(excludeUserId int, keyword string) ([]map[string]any, error) {
	keyword = strings.TrimSpace(keyword)
	if excludeUserId <= 0 || keyword == "" || len(keyword) > 64 {
		return []map[string]any{}, nil
	}
	query := DB.Model(&User{}).Select("id", "username", "display_name").Where("status = ? AND id <> ?", common.UserStatusEnabled, excludeUserId)
	if id, err := strconv.Atoi(keyword); err == nil && id > 0 {
		query = query.Where("(id = ? OR username = ?)", id, keyword)
	} else {
		query = query.Where("username = ?", keyword)
	}
	var users []User
	if err := query.Order("id asc").Limit(10).Find(&users).Error; err != nil {
		return nil, err
	}
	recipients := make([]map[string]any, 0, len(users))
	for _, user := range users {
		recipients = append(recipients, map[string]any{"id": user.Id, "username": user.Username, "display_name": user.DisplayName})
	}
	return recipients, nil
}

// GetWalletTransfersForUser lists transfers the user sent or received,
// optionally only the one with the given order number.
func GetWalletTransfersForUser(userId int, tradeNo string, startIdx, limit int) ([]WalletTransfer, int64, error) {
	if userId <= 0 || startIdx < 0 || limit < 1 || limit > 100 || len(tradeNo) > 64 {
		return nil, 0, ErrWalletTransferInvalid
	}
	query := DB.Model(&WalletTransfer{}).Where("(sender_id = ? OR recipient_id = ?)", userId, userId)
	if strings.TrimSpace(tradeNo) != "" {
		dateKey, id, ok := ParseWalletTransferTradeNo(tradeNo)
		if !ok {
			return []WalletTransfer{}, 0, nil
		}
		query = query.Where("id = ? AND date_key = ?", id, dateKey)
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	records := []WalletTransfer{}
	if err := query.Session(&gorm.Session{}).Order("created_at desc").Order("id desc").Offset(startIdx).Limit(limit).Find(&records).Error; err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

func DeleteExpiredWalletTransferState(now time.Time) error {
	if !DB.Migrator().HasTable(&WalletTransferAttempt{}) || !DB.Migrator().HasTable(&WalletTransferDailyConfig{}) {
		return nil
	}
	cutoff := walletTransferDateKey(now.AddDate(0, 0, -WalletTransferStateRetentionDays))
	return DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("date_key < ?", cutoff).Delete(&WalletTransferAttempt{}).Error; err != nil {
			return err
		}
		return tx.Where("date_key < ?", cutoff).Delete(&WalletTransferDailyConfig{}).Error
	})
}

func SearchWalletTransfers(userId int, keyword, dateKey string, startIdx, limit int) ([]WalletTransfer, int64, int64, error) {
	if userId < 0 || startIdx < 0 || limit < 1 || limit > 100 || len(keyword) > 64 || len(dateKey) > 10 {
		return nil, 0, 0, ErrWalletTransferInvalid
	}
	query := DB.Model(&WalletTransfer{})
	if userId > 0 {
		query = query.Where("(sender_id = ? OR recipient_id = ?)", userId, userId)
	}
	if tradeDateKey, tradeId, ok := ParseWalletTransferTradeNo(keyword); ok {
		query = query.Where("id = ? AND date_key = ?", tradeId, tradeDateKey)
	} else if keyword != "" {
		if id, err := strconv.Atoi(keyword); err == nil && id > 0 {
			query = query.Where("(sender_id = ? OR recipient_id = ? OR sender_username LIKE ? OR recipient_username LIKE ?)", id, id, "%"+keyword+"%", "%"+keyword+"%")
		} else {
			query = query.Where("(sender_username LIKE ? OR recipient_username LIKE ?)", "%"+keyword+"%", "%"+keyword+"%")
		}
	}
	if dateKey != "" {
		if _, err := time.Parse("2006-01-02", dateKey); err != nil {
			return nil, 0, 0, ErrWalletTransferInvalid
		}
		query = query.Where("date_key = ?", dateKey)
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, 0, err
	}
	var totalFee int64
	row := query.Session(&gorm.Session{}).Select("COALESCE(SUM(fee), 0)").Row()
	if err := row.Scan(&totalFee); err != nil {
		return nil, 0, 0, err
	}
	var records []WalletTransfer
	if err := query.Session(&gorm.Session{}).Order("created_at desc").Order("id desc").Offset(startIdx).Limit(limit).Find(&records).Error; err != nil {
		return nil, 0, 0, err
	}
	return records, total, totalFee, nil
}

func checkedWalletTransferLimit(rate float64, displayType string) (int64, error) {
	if math.IsNaN(rate) || math.IsInf(rate, 0) || rate <= 0 {
		return 0, ErrWalletTransferInvalid
	}
	quota := decimal.NewFromInt(int64(WalletTransferDailyLimit)).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	// Token display has no currency of its own, so the cap is 100 USD worth of
	// quota there rather than 100 raw tokens.
	if displayType != operation_setting.QuotaDisplayTypeTokens {
		quota = quota.Div(decimal.NewFromFloat(rate))
	}
	quota = quota.Floor()
	value, err := common.WalletQuotaFromDecimalStrict(quota)
	if err != nil || value <= 0 {
		return 0, ErrWalletTransferInvalid
	}
	return min(int64(value), int64(common.MaxQuota)), nil
}
