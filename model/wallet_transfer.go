package model

import (
	"context"
	"crypto/subtle"
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/operation_setting"
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
	ErrWalletTransferInvalid          = errors.New("Wallet transfer request is invalid.")
	ErrWalletTransferRecipientInvalid = errors.New("The recipient is unavailable.")
	ErrWalletTransferInsufficient     = errors.New("Insufficient wallet balance.")
	ErrWalletTransferBelowMinimum     = errors.New("The transfer amount is below the minimum of 1.")
	ErrWalletTransferDailyLimit       = errors.New("The daily wallet transfer limit has been reached.")
	ErrWalletTransferAttemptLimit     = errors.New("The daily wallet transfer attempt limit has been reached.")
	ErrWalletTransferPasswordRequired = errors.New("Set a payment password before transferring balance.")
	ErrWalletTransferLoginRequired    = errors.New("Set a login password before transferring balance.")
	ErrWalletTransferPasswordInvalid  = errors.New("The login password or payment password is incorrect.")
	ErrWalletTransferLoginInvalid     = errors.New("The login password is incorrect.")
	ErrWalletTransferQuoteInvalid     = errors.New("This transfer review has expired or was already used. Review the transfer again.")
	ErrWalletTransferPasswordLocked   = errors.New("Payment password verification is temporarily locked.")
	ErrWalletTransferTwoFAInvalid     = errors.New("The two-factor authentication code is incorrect.")
	ErrWalletTransferTwoFALocked      = errors.New("Two-factor authentication is temporarily locked.")
	ErrWalletTransferTwoFAReplay      = errors.New("This two-factor authentication code has already been used.")
	ErrWalletPasswordFlowInvalid      = errors.New("The email verification flow is invalid or expired.")
	ErrWalletPasswordCodeInvalid      = errors.New("The email verification code is incorrect.")
	ErrWalletPasswordFlowLocked       = errors.New("Too many incorrect verification codes. Start again.")
	ErrWalletPasswordCurrentInvalid   = errors.New("The current payment password is incorrect.")
	ErrWalletPasswordSameAsLogin      = errors.New("The payment password must differ from the login password.")
	ErrWalletPasswordUnchanged        = errors.New("The new payment password must differ from the current payment password.")
	ErrWalletPasswordStateChanged     = errors.New("Payment password settings changed. Start again.")
	ErrWalletTransferPasswordState    = ErrWalletPasswordStateChanged
	ErrEmailBindingRequired           = errors.New("Bind an email address before managing the payment password.")
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

// The root user can already grant balance to anyone, so it may manage its
// payment password without binding an email. Every other check still applies.
func walletPasswordEmailExempt(user *User) bool {
	return user.Role == common.RoleRootUser && NormalizeEmail(user.Email) == ""
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
	if amount <= 0 || amount > common.MaxQuota || !operation_setting.ValidateWalletTransferFeePercent(feePercent) {
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
	if identity.UserID <= 0 || identity.SessionID == "" || recipientId <= 0 || amount <= 0 || amount > common.MaxQuota || dailyLimitQuota <= 0 || dailyLimitQuota > common.MaxQuota || dailyLimitAmount != WalletTransferDailyLimit || len(displayType) > 16 || len(displaySymbol) > 16 {
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
		email = NormalizeEmail(user.Email)
		emailExempt := walletPasswordEmailExempt(&user)
		if email == "" && !emailExempt {
			return ErrEmailBindingRequired
		}
		if emailExempt {
			codeHash = ""
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
		state, err := common.Marshal(walletPasswordFlowState{Intent: intent, Email: email, CodeHash: codeHash, EmailExempt: emailExempt})
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
	userQuotaBatchFlushMu.Lock()
	defer userQuotaBatchFlushMu.Unlock()

	var transfer WalletTransfer
	var outcome error
	var pending map[int]int
	var cacheReserved bool
	var cacheReservationAttempted bool
	var cacheGeneration string
	var reservationAmount int
	var recipientCacheCredit quotaCreditCache
	var succeeded bool
	var alreadyCompleted bool
	var senderId int
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
		senderId = sender.Id
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
		loginValid := common.ValidatePasswordAndHash(input.LoginPassword, sender.Password)
		paymentValid := common.ValidatePasswordAndHash(input.PaymentPassword, security.PasswordHash)
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
		if quote.RecipientId == sender.Id || quote.RecipientUsername != recipient.Username || quote.RecipientDisplayName != recipient.DisplayName || quote.Amount > common.MaxQuota || quote.Received > common.MaxQuota {
			return ErrWalletTransferRecipientInvalid
		}
		if common.RedisEnabled {
			cacheReservationAttempted = true
			reservationAmount = int(quote.Amount)
			cacheGeneration, _ = common.RDB.HGet(context.Background(), getUserCacheKey(sender.Id), "QuotaGeneration").Result()
			reserveResult, reserveErr := cacheTryReserveUserQuota(sender.Id, quote.Amount)
			if reserveErr == nil && reserveResult == cacheQuotaMiss {
				if _, hydrateErr := GetUserCache(sender.Id); hydrateErr == nil {
					cacheGeneration, _ = common.RDB.HGet(context.Background(), getUserCacheKey(sender.Id), "QuotaGeneration").Result()
					reserveResult, reserveErr = cacheTryReserveUserQuota(sender.Id, quote.Amount)
				}
			}
			cacheReserved = reserveErr == nil && reserveResult == cacheQuotaOK
		}
		pending = takeBatchUserQuotaDeltas(sender.Id, recipient.Id)
		senderEffective := int64(sender.Quota) + int64(pending[sender.Id])
		recipientEffective := int64(recipient.Quota) + int64(pending[recipient.Id])
		if senderEffective < quote.Amount {
			outcome = ErrWalletTransferInsufficient
			return nil
		}
		if recipientEffective < 0 || recipientEffective > common.MaxQuota-quote.Received {
			return ErrWalletQuotaLimitExceeded
		}
		newSenderQuota := senderEffective - quote.Amount
		newRecipientQuota := recipientEffective + quote.Received
		if newSenderQuota < 0 || newSenderQuota > common.MaxQuota || newRecipientQuota > common.MaxQuota {
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
	if len(pending) > 0 && (!succeeded || err != nil) {
		restoreBatchUserQuotaDeltas(pending)
	}
	if cacheReservationAttempted && (!succeeded || err != nil) {
		if cacheReserved {
			releaseWalletTransferCacheReservation(identity.UserID, reservationAmount, cacheGeneration)
		} else {
			// A Redis timeout can happen after the reservation script ran. Drop the
			// possibly stale cache whenever no database transfer was committed.
			if invalidateErr := invalidateUserCache(identity.UserID); invalidateErr != nil {
				common.SysError(common.LogText("failed to invalidate sender cache after wallet transfer rollback: %s", invalidateErr.Error()))
			}
		}
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
	RecordLog(transfer.SenderId, LogTypeSystem, common.NewMessage("Transferred {{quota}} to {{username}} (ID {{id}}), fee {{fee}}", map[string]any{
		"quota": logger.FormatQuota(int(transfer.Amount)), "username": transfer.RecipientUsername,
		"id": transfer.RecipientId, "fee": logger.FormatQuota(int(transfer.Fee)),
	}))
	RecordLog(transfer.RecipientId, LogTypeSystem, common.NewMessage("Received {{quota}} from {{username}} (ID {{id}})", map[string]any{
		"quota": logger.FormatQuota(int(transfer.Received)), "username": transfer.SenderUsername, "id": transfer.SenderId,
	}))
	if common.RedisEnabled {
		syncCreditUserQuotaCache(transfer.RecipientId, int(transfer.Received), "wallet transfer", recipientCacheCredit)
		if !cacheReserved {
			if err := invalidateUserCache(senderId); err != nil {
				common.SysError(common.LogText("failed to invalidate sender cache after wallet transfer: %s", err.Error()))
			}
		}
	}
	return &transfer, nil
}

func releaseWalletTransferCacheReservation(userId, amount int, generation string) {
	if !common.RedisEnabled || amount <= 0 || generation == "" {
		return
	}
	const script = `
if redis.call('HGET', KEYS[1], 'QuotaGeneration') ~= ARGV[1]
  or redis.call('HEXISTS', KEYS[1], 'Quota') == 0 then
  return 0
end
redis.call('HINCRBY', KEYS[1], 'Quota', ARGV[2])
return 1`
	if err := common.RDB.Eval(context.Background(), script, []string{getUserCacheKey(userId)}, generation, amount).Err(); err != nil {
		common.SysError(common.LogText("failed to restore wallet balance after transfer rollback: %s", err.Error()))
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

func GetWalletTransfersForUser(userId, startIdx, limit int) ([]WalletTransfer, int64, error) {
	if userId <= 0 || startIdx < 0 || limit < 1 || limit > 100 {
		return nil, 0, ErrWalletTransferInvalid
	}
	query := DB.Model(&WalletTransfer{}).Where("(sender_id = ? OR recipient_id = ?)", userId, userId)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var records []WalletTransfer
	if err := query.Order("created_at desc").Order("id desc").Offset(startIdx).Limit(limit).Find(&records).Error; err != nil {
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
	if keyword != "" {
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
