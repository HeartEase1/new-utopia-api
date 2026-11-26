package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/pquerna/otp/totp"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func prepareWalletTransferTest(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&WalletSecurity{}, &WalletTransferDailyConfig{}, &WalletTransferAttempt{}, &WalletTransfer{}))
	truncateTables(t)
	for _, table := range []any{&WalletSecurity{}, &WalletTransferDailyConfig{}, &WalletTransferAttempt{}, &WalletTransfer{}} {
		require.NoError(t, DB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(table).Error)
	}
	oldRedisEnabled := common.RedisEnabled
	oldBatchUpdateEnabled := common.BatchUpdateEnabled
	common.RedisEnabled = false
	common.BatchUpdateEnabled = false
	t.Cleanup(func() {
		common.RedisEnabled = oldRedisEnabled
		common.BatchUpdateEnabled = oldBatchUpdateEnabled
	})
}

func createWalletTransferTestAccount(t *testing.T, username string, quota int, withPaymentPassword bool) (User, AuthSessionIdentity, string, string) {
	t.Helper()
	loginPassword := "correct horse battery staple"
	loginHash, err := common.Password2Hash(loginPassword)
	require.NoError(t, err)
	user := User{
		Username: username, Password: loginHash, Status: common.UserStatusEnabled,
		Role: common.RoleCommonUser, Group: "default", AuthVersion: 1, Quota: quota,
		AffCode: "aff-" + common.GetRandomString(12), Email: username + "@example.com",
	}
	require.NoError(t, DB.Create(&user).Error)
	session := newTestUserSession("wallet-transfer-"+username, user.Id, time.Now().Unix())
	require.NoError(t, CreateUserSession(session))
	identity := AuthSessionIdentity{UserID: user.Id, SessionID: session.SID, UserAuthVersion: 1, SessionVersion: 1}
	paymentPassword := "secure payment password"
	if withPaymentPassword {
		passwordHash, hashErr := common.Password2Hash(paymentPassword)
		require.NoError(t, hashErr)
		require.NoError(t, DB.Create(&WalletSecurity{UserId: user.Id, PasswordHash: passwordHash}).Error)
	}
	return user, identity, loginPassword, paymentPassword
}

func createWalletTransferTestQuote(t *testing.T, identity AuthSessionIdentity, recipientId int, amount int64, feePercent float64) *WalletTransferQuoteResult {
	t.Helper()
	result, err := CreateWalletTransferQuote(identity, recipientId, amount, feePercent, 1_000, 100, "USD", "$ ")
	require.NoError(t, err)
	return result
}

func TestWalletTransferMovesBalanceOnceAndRecordsRoundedFee(t *testing.T) {
	prepareWalletTransferTest(t)
	sender, identity, loginPassword, paymentPassword := createWalletTransferTestAccount(t, "transfer-sender", 100, true)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "transfer-recipient", 10, false)
	quote := createWalletTransferTestQuote(t, identity, recipient.Id, 10, 1.01)
	assert.Equal(t, int64(1), quote.Quote.Fee, "the fee rounds up to the next quota unit")
	assert.Equal(t, int64(9), quote.Quote.Received)

	transfer, err := ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(10), transfer.Amount)
	assert.Equal(t, int64(1), transfer.Fee)
	assert.Equal(t, int64(9), transfer.Received)
	assert.Equal(t, 90, getUserQuotaFromDB(t, sender.Id))
	assert.Equal(t, 19, getUserQuotaFromDB(t, recipient.Id))

	replayed, err := ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
	})
	require.NoError(t, err)
	assert.Equal(t, transfer.Id, replayed.Id)
	assert.Equal(t, 90, getUserQuotaFromDB(t, sender.Id), "retrying the same quote must not debit twice")
	assert.Equal(t, 19, getUserQuotaFromDB(t, recipient.Id), "retrying the same quote must not credit twice")
	var transferCount int64
	require.NoError(t, DB.Model(&WalletTransfer{}).Count(&transferCount).Error)
	assert.EqualValues(t, 1, transferCount)
}

func TestWalletTransferFeeExemptionRequiresCurrentRootSender(t *testing.T) {
	for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
		t.Run(strconv.Itoa(role), func(t *testing.T) {
			prepareWalletTransferTest(t)
			sender, identity, login, payment := createWalletTransferTestAccount(t, "fee-sender", 100, true)
			recipient, _, _, _ := createWalletTransferTestAccount(t, "fee-recipient", 0, false)
			require.NoError(t, DB.Model(&User{}).Where("id = ?", sender.Id).Update("role", role).Error)
			// Receiving into a root account must not exempt a non-root sender.
			require.NoError(t, DB.Model(&User{}).Where("id = ?", recipient.Id).Update("role", common.RoleRootUser).Error)
			settings := operation_setting.GetPaymentSetting()
			oldFee := settings.WalletTransferFeePercent
			settings.WalletTransferFeePercent = 2
			t.Cleanup(func() { settings.WalletTransferFeePercent = oldFee })
			wantFee, wantRate := int64(1), "2.00"
			if role == common.RoleRootUser {
				wantFee, wantRate = 0, "0.00"
			}
			status, err := GetWalletTransferStatus(sender.Id, 1000, "USD", "$")
			require.NoError(t, err)
			assert.Equal(t, wantRate, status.FeePercent)
			quote := createWalletTransferTestQuote(t, identity, recipient.Id, 50, 2)
			assert.Equal(t, wantFee, quote.Quote.Fee)
			assert.Equal(t, wantRate, quote.Quote.FeePercent)
			submit := WalletTransferSubmit{QuoteToken: quote.QuoteToken, LoginPassword: login, PaymentPassword: payment}
			transfer, err := ConfirmWalletTransfer(identity, submit)
			require.NoError(t, err)
			assert.Equal(t, wantFee, transfer.Fee)
			assert.Equal(t, wantRate, transfer.FeePercent)
			assert.Equal(t, int64(50)-wantFee, transfer.Received)
			replayed, err := ConfirmWalletTransfer(identity, submit)
			require.NoError(t, err)
			assert.Equal(t, transfer.Id, replayed.Id)
			assert.Equal(t, 50, getUserQuotaFromDB(t, sender.Id))
			assert.Equal(t, 50-int(wantFee), getUserQuotaFromDB(t, recipient.Id))
		})
	}
	for _, role := range []int{common.RoleCommonUser, common.RoleRootUser} {
		t.Run("role-change-"+strconv.Itoa(role), func(t *testing.T) {
			prepareWalletTransferTest(t)
			sender, identity, login, payment := createWalletTransferTestAccount(t, "role-sender", 100, true)
			recipient, _, _, _ := createWalletTransferTestAccount(t, "role-recipient", 0, false)
			require.NoError(t, DB.Model(&User{}).Where("id = ?", sender.Id).Update("role", role).Error)
			quote := createWalletTransferTestQuote(t, identity, recipient.Id, 50, 2)
			nextRole := common.RoleRootUser
			if role == common.RoleRootUser {
				nextRole = common.RoleAdminUser
			}
			require.NoError(t, DB.Model(&User{}).Where("id = ?", sender.Id).Update("role", nextRole).Error)
			_, err := ConfirmWalletTransfer(identity, WalletTransferSubmit{QuoteToken: quote.QuoteToken, LoginPassword: login, PaymentPassword: payment})
			assert.ErrorIs(t, err, ErrWalletTransferQuoteInvalid)
			assert.Equal(t, 100, getUserQuotaFromDB(t, sender.Id))
			assert.Zero(t, getUserQuotaFromDB(t, recipient.Id))
		})
	}
}

func TestWalletTransferRequiresFreshTOTPAndCapsFailedSubmissions(t *testing.T) {
	prepareWalletTransferTest(t)
	sender, identity, loginPassword, paymentPassword := createWalletTransferTestAccount(t, "totp-sender", 100, false)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "totp-recipient", 10, false)
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "New Utopia", AccountName: sender.Username})
	require.NoError(t, err)
	require.NoError(t, DB.Create(&WalletSecurity{UserId: sender.Id, PasswordHash: mustWalletPasswordHash(t, paymentPassword)}).Error)
	require.NoError(t, DB.Create(&TwoFA{UserId: sender.Id, Secret: key.Secret(), IsEnabled: true}).Error)
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	require.NoError(t, err)
	firstQuote := createWalletTransferTestQuote(t, identity, recipient.Id, 10, 0)
	_, err = ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: firstQuote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword, TwoFACode: code,
	})
	require.NoError(t, err)
	secondQuote := createWalletTransferTestQuote(t, identity, recipient.Id, 10, 0)
	_, err = ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: secondQuote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword, TwoFACode: code,
	})
	assert.ErrorIs(t, err, ErrWalletTransferTwoFAReplay)
	assert.Equal(t, 90, getUserQuotaFromDB(t, sender.Id))
	assert.Equal(t, 20, getUserQuotaFromDB(t, recipient.Id))

	missingPasswordSender, missingPasswordIdentity, _, _ := createWalletTransferTestAccount(t, "attempt-sender", 100, false)
	attemptRecipient, _, _, _ := createWalletTransferTestAccount(t, "attempt-recipient", 0, false)
	for range 10 {
		quote := createWalletTransferTestQuote(t, missingPasswordIdentity, attemptRecipient.Id, 10, 0)
		_, err := ConfirmWalletTransfer(missingPasswordIdentity, WalletTransferSubmit{QuoteToken: quote.QuoteToken})
		assert.ErrorIs(t, err, ErrWalletTransferPasswordRequired)
	}
	lastQuote := createWalletTransferTestQuote(t, missingPasswordIdentity, attemptRecipient.Id, 10, 0)
	_, err = ConfirmWalletTransfer(missingPasswordIdentity, WalletTransferSubmit{QuoteToken: lastQuote.QuoteToken})
	assert.ErrorIs(t, err, ErrWalletTransferAttemptLimit)
	var attemptCount int64
	require.NoError(t, DB.Model(&WalletTransferAttempt{}).Where("user_id = ?", missingPasswordSender.Id).Count(&attemptCount).Error)
	assert.EqualValues(t, 10, attemptCount)
}

func TestWalletTransferDailyCapAndTOTPExemption(t *testing.T) {
	prepareWalletTransferTest(t)
	sender, identity, loginPassword, paymentPassword := createWalletTransferTestAccount(t, "limited-sender", 100, true)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "limited-recipient", 0, false)
	now := time.Now()
	require.NoError(t, DB.Create(&WalletTransfer{
		QuoteFlowId: 700001, SenderId: sender.Id, SenderUsername: sender.Username,
		RecipientId: recipient.Id, RecipientUsername: recipient.Username,
		Amount: 990, Received: 990, FeePercent: "0.00", DateKey: walletTransferDateKey(now), CreatedAt: now,
	}).Error)
	quote := createWalletTransferTestQuote(t, identity, recipient.Id, 11, 0)
	_, err := ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
	})
	assert.ErrorIs(t, err, ErrWalletTransferDailyLimit)
	assert.Equal(t, 100, getUserQuotaFromDB(t, sender.Id))

	// An administrator is limited like everyone else; only the root user is not.
	require.NoError(t, DB.Model(&User{}).Where("id = ?", sender.Id).Update("role", common.RoleAdminUser).Error)
	limitedQuote := createWalletTransferTestQuote(t, identity, recipient.Id, 11, 0)
	_, err = ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: limitedQuote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
	})
	assert.ErrorIs(t, err, ErrWalletTransferDailyLimit)
	require.NoError(t, DB.Where("user_id = ?", sender.Id).Delete(&WalletTransferAttempt{}).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", sender.Id).Update("role", common.RoleRootUser).Error)

	// One transfer sends at least one display unit: a hundredth of the daily cap.
	_, err = CreateWalletTransferQuote(identity, recipient.Id, 9, 0, 1_000, 100, "USD", "$ ")
	assert.ErrorIs(t, err, ErrWalletTransferBelowMinimum)
	for i := range 10 {
		require.NoError(t, DB.Create(&WalletTransferAttempt{
			QuoteFlowId: int64(710000 + i), UserId: sender.Id, DateKey: walletTransferDateKey(now), Quota: 1, SubmittedAt: now,
		}).Error)
	}
	adminQuote := createWalletTransferTestQuote(t, identity, recipient.Id, 11, 0)
	_, err = ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: adminQuote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
	})
	require.NoError(t, err)
	assert.Equal(t, 89, getUserQuotaFromDB(t, sender.Id))
	adminStatus, err := GetWalletTransferStatus(sender.Id, 1_000, "USD", "$ ")
	require.NoError(t, err)
	assert.True(t, adminStatus.LimitsExempt)

	secureSender, secureIdentity, secureLoginPassword, securePaymentPassword := createWalletTransferTestAccount(t, "secure-sender", 100, true)
	secureRecipient, _, _, _ := createWalletTransferTestAccount(t, "secure-recipient", 0, false)
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "New Utopia", AccountName: secureSender.Username})
	require.NoError(t, err)
	require.NoError(t, DB.Create(&TwoFA{UserId: secureSender.Id, Secret: key.Secret(), IsEnabled: true}).Error)
	require.NoError(t, DB.Create(&WalletTransfer{
		QuoteFlowId: 700002, SenderId: secureSender.Id, SenderUsername: secureSender.Username,
		RecipientId: secureRecipient.Id, RecipientUsername: secureRecipient.Username,
		Amount: 1_000, Received: 1_000, FeePercent: "0.00", DateKey: walletTransferDateKey(now), CreatedAt: now,
	}).Error)
	code, err := totp.GenerateCode(key.Secret(), now)
	require.NoError(t, err)
	secureQuote := createWalletTransferTestQuote(t, secureIdentity, secureRecipient.Id, 10, 0)
	transfer, err := ConfirmWalletTransfer(secureIdentity, WalletTransferSubmit{
		QuoteToken: secureQuote.QuoteToken, LoginPassword: secureLoginPassword,
		PaymentPassword: securePaymentPassword, TwoFACode: code,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(10), transfer.Received)
	assert.Equal(t, 90, getUserQuotaFromDB(t, secureSender.Id))
	assert.Equal(t, 10, getUserQuotaFromDB(t, secureRecipient.Id))
}

func TestWalletTransferRecipientSearchNeedsCompleteUsernameOrID(t *testing.T) {
	prepareWalletTransferTest(t)
	sender, _, _, _ := createWalletTransferTestAccount(t, "search-sender", 0, false)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "search_target", 0, false)
	createWalletTransferTestAccount(t, "searchXtarget", 0, false)
	disabled, _, _, _ := createWalletTransferTestAccount(t, "search-disabled", 0, false)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", disabled.Id).Update("status", common.UserStatusDisabled).Error)

	for keyword, want := range map[string][]any{
		"search_target": {recipient.Id}, strconv.Itoa(recipient.Id): {recipient.Id}, " search_target ": {recipient.Id},
		"search": {}, "search_targe": {}, "search%": {}, "search_targe_": {}, "%": {}, "": {},
		"search-sender": {}, strconv.Itoa(sender.Id): {}, "search-disabled": {},
	} {
		found, err := SearchWalletTransferRecipients(sender.Id, keyword)
		require.NoError(t, err)
		ids := make([]any, 0, len(found))
		for _, item := range found {
			ids = append(ids, item["id"])
		}
		assert.Equal(t, want, ids, "keyword %q", keyword)
	}
}

func TestWalletTransferHistoryShowsOrderNumbersAndFindsOneByIt(t *testing.T) {
	prepareWalletTransferTest(t)
	sender, identity, loginPassword, paymentPassword := createWalletTransferTestAccount(t, "history-sender", 100, true)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "history-recipient", 0, false)
	outsider, _, _, _ := createWalletTransferTestAccount(t, "history-outsider", 0, false)
	var transfers []*WalletTransfer
	for range 2 {
		quote := createWalletTransferTestQuote(t, identity, recipient.Id, 10, 2)
		transfer, err := ConfirmWalletTransfer(identity, WalletTransferSubmit{
			QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
		})
		require.NoError(t, err)
		transfers = append(transfers, transfer)
	}
	first := transfers[0]
	assert.Equal(t, WalletTransferTradeNo(first.DateKey, first.Id), first.TradeNo)
	assert.Regexp(t, `^WT\d{8}\d{8,}$`, first.TradeNo)

	// Both sides see every record with its order number, newest first.
	for _, userId := range []int{sender.Id, recipient.Id} {
		records, total, err := GetWalletTransfersForUser(userId, "", 0, 20)
		require.NoError(t, err)
		assert.Equal(t, int64(2), total)
		require.Len(t, records, 2)
		assert.Equal(t, transfers[1].Id, records[0].Id)
		assert.Equal(t, transfers[1].TradeNo, records[0].TradeNo)
		assert.Equal(t, int64(10), records[0].Amount)
	}

	// An order number, in any letter case, finds only that transfer, and only for its parties.
	records, total, err := GetWalletTransfersForUser(sender.Id, strings.ToLower(first.TradeNo), 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, records, 1)
	assert.Equal(t, first.Id, records[0].Id)
	records, _, err = GetWalletTransfersForUser(outsider.Id, first.TradeNo, 0, 20)
	require.NoError(t, err)
	assert.Empty(t, records)
	records, _, err = GetWalletTransfersForUser(sender.Id, "WT2099010100000001", 0, 20)
	require.NoError(t, err)
	assert.Empty(t, records)
	adminRecords, adminTotal, _, err := SearchWalletTransfers(0, first.TradeNo, "", 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(1), adminTotal)
	require.Len(t, adminRecords, 1)
	assert.Equal(t, first.TradeNo, adminRecords[0].TradeNo)

	for _, invalid := range []string{"", "WT", "WT20261301" + "00000001", "XX2026010100000001", "WT2026010100000000"} {
		_, _, ok := ParseWalletTransferTradeNo(invalid)
		assert.False(t, ok, invalid)
	}
}

// A model request reserves quota in the Redis cache before the database
// catches up. The transfer must not spend that reserved balance again.
func TestWalletTransferRespectsQuotaReservedInTheCache(t *testing.T) {
	prepareWalletTransferTest(t)
	server := miniredis.RunT(t)
	oldRedisEnabled, oldRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled, common.RDB = true, redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled, common.RDB = oldRedisEnabled, oldRDB
	})
	sender, identity, loginPassword, paymentPassword := createWalletTransferTestAccount(t, "reserved-sender", 100, true)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "reserved-recipient", 0, false)
	_, err := GetUserCache(sender.Id)
	require.NoError(t, err)
	reserved, err := cacheTryReserveUserQuota(sender.Id, 80)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, reserved)

	quote := createWalletTransferTestQuote(t, identity, recipient.Id, 50, 0)
	_, err = ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
	})
	assert.ErrorIs(t, err, ErrWalletTransferInsufficient)
	assert.Equal(t, 100, getUserQuotaFromDB(t, sender.Id))
	assert.Equal(t, 0, getUserQuotaFromDB(t, recipient.Id))
	cachedQuota, err := getUserQuotaCache(sender.Id)
	require.NoError(t, err)
	assert.Equal(t, 20, cachedQuota, "a refused transfer leaves the reservation untouched")

	// What is still free can be sent.
	quote = createWalletTransferTestQuote(t, identity, recipient.Id, 20, 0)
	_, err = ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
	})
	require.NoError(t, err)
	assert.Equal(t, 80, getUserQuotaFromDB(t, sender.Id), "the reserved 80 is still to be written")
	assert.Equal(t, 20, getUserQuotaFromDB(t, recipient.Id))
}

// When Redis cannot answer, the reservations it holds are unknown, so the
// transfer is refused rather than trusting a database balance that may not
// include them yet.
func TestWalletTransferIsRefusedWhileTheCachedBalanceCannotBeChecked(t *testing.T) {
	prepareWalletTransferTest(t)
	server := miniredis.RunT(t)
	oldRedisEnabled, oldRDB := common.RedisEnabled, common.RDB
	common.RedisEnabled, common.RDB = true, redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled, common.RDB = oldRedisEnabled, oldRDB
	})
	sender, identity, loginPassword, paymentPassword := createWalletTransferTestAccount(t, "unverified-sender", 100, true)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "unverified-recipient", 0, false)
	_, err := GetUserCache(sender.Id)
	require.NoError(t, err)
	reserved, err := cacheTryReserveUserQuota(sender.Id, 80)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, reserved)
	quote := createWalletTransferTestQuote(t, identity, recipient.Id, 50, 0)

	server.Close()
	_, err = ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
	})

	assert.ErrorIs(t, err, ErrWalletTransferBalanceUnverified)
	assert.Equal(t, 100, getUserQuotaFromDB(t, sender.Id))
	assert.Equal(t, 0, getUserQuotaFromDB(t, recipient.Id))
}

// failReservationOnce makes the next cache reservation fail like a Redis
// timeout, either before the script runs or after it has run. It is armed
// once *failed is set to false.
type failReservationOnce struct {
	afterRun bool
	failed   *bool
}

func (hook failReservationOnce) isReservation(cmd redis.Cmder) bool {
	args := cmd.Args()
	return !*hook.failed && cmd.Name() == "eval" && len(args) > 1 && strings.Contains(fmt.Sprint(args[1]), "-tonumber(ARGV[1])")
}

func (hook failReservationOnce) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	if !hook.afterRun && hook.isReservation(cmd) {
		*hook.failed = true
		return ctx, errors.New("i/o timeout")
	}
	return ctx, nil
}

func (hook failReservationOnce) AfterProcess(_ context.Context, cmd redis.Cmder) error {
	if hook.afterRun && hook.isReservation(cmd) {
		*hook.failed = true
		cmd.SetErr(errors.New("i/o timeout"))
	}
	return nil
}

func (failReservationOnce) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (failReservationOnce) AfterProcessPipeline(context.Context, []redis.Cmder) error { return nil }

func useWalletTransferTestRedis(t *testing.T, hook redis.Hook) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	oldRedisEnabled, oldRDB := common.RedisEnabled, common.RDB
	client := redis.NewClient(&redis.Options{Addr: server.Addr(), MaxRetries: -1})
	client.AddHook(hook)
	common.RedisEnabled, common.RDB = true, client
	t.Cleanup(func() {
		_ = common.RDB.Close()
		common.RedisEnabled, common.RDB = oldRedisEnabled, oldRDB
	})
	return server
}

// failScriptOnce fails the next Lua script containing match before it runs.
type failScriptOnce struct {
	match  string
	failed *bool
}

func (hook failScriptOnce) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	args := cmd.Args()
	if !*hook.failed && cmd.Name() == "eval" && len(args) > 1 && strings.Contains(fmt.Sprint(args[1]), hook.match) {
		*hook.failed = true
		return ctx, errors.New("i/o timeout")
	}
	return ctx, nil
}

func (failScriptOnce) AfterProcess(context.Context, redis.Cmder) error { return nil }

func (failScriptOnce) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}

func (failScriptOnce) AfterProcessPipeline(context.Context, []redis.Cmder) error { return nil }

func walletTransferReservationKeys(server *miniredis.Miniredis) []string {
	return lo.Filter(server.Keys(), func(key string, _ int) bool {
		return strings.HasPrefix(key, "wallet_transfer:reservation:")
	})
}

// A transfer that failed in the database keeps its reservation until a late
// release gives it back. A retry with the same review must take its own
// reservation, so that late release cannot hand back what the retry spends.
func TestWalletTransferRetryNeverReusesTheReservationOfAFailedAttempt(t *testing.T) {
	prepareWalletTransferTest(t)
	releaseFailed := true
	server := useWalletTransferTestRedis(t, failScriptOnce{match: "redis.call('DEL', KEYS[2])", failed: &releaseFailed})
	sender, identity, loginPassword, paymentPassword := createWalletTransferTestAccount(t, "retry-sender", 100, true)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "retry-recipient", 0, false)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", sender.Id).Update("role", common.RoleRootUser).Error)
	_, err := GetUserCache(sender.Id)
	require.NoError(t, err)
	quote := createWalletTransferTestQuote(t, identity, recipient.Id, 50, 0)
	submit := WalletTransferSubmit{QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword}

	// The database write fails after the cache reserved 50, and the immediate
	// release fails too, so that reservation stays for a later release.
	require.NoError(t, DB.Migrator().DropTable(&WalletTransfer{}))
	releaseFailed = false
	_, err = ConfirmWalletTransfer(identity, submit)
	require.Error(t, err)
	require.True(t, releaseFailed)
	require.NoError(t, DB.AutoMigrate(&WalletTransfer{}))
	failedAttempt := walletTransferReservationKeys(server)
	require.Len(t, failedAttempt, 1)

	_, err = ConfirmWalletTransfer(identity, submit)
	require.NoError(t, err)
	cached, err := getUserQuotaCache(sender.Id)
	require.NoError(t, err)
	assert.Equal(t, 0, cached, "the retry took its own 50 next to the one still held")

	// The late release of the failed attempt gives back only its own 50.
	require.NoError(t, common.RDB.Eval(context.Background(), walletTransferReleaseScript,
		[]string{getUserCacheKey(sender.Id), failedAttempt[0]}).Err())
	cached, err = getUserQuotaCache(sender.Id)
	require.NoError(t, err)
	assert.Equal(t, 50, cached)
	assert.Equal(t, 50, getUserQuotaFromDB(t, sender.Id))
	assert.Equal(t, 50, getUserQuotaFromDB(t, recipient.Id))
}

// A Redis timeout during the transfer must not drop other reservations from
// the cache, or a retry would spend them again.
func TestWalletTransferTimeoutKeepsReservationsOfRunningRequests(t *testing.T) {
	prepareWalletTransferTest(t)
	failed := true
	useWalletTransferTestRedis(t, failReservationOnce{failed: &failed})
	sender, identity, loginPassword, paymentPassword := createWalletTransferTestAccount(t, "timeout-sender", 100, true)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "timeout-recipient", 0, false)
	_, err := GetUserCache(sender.Id)
	require.NoError(t, err)
	reserved, err := cacheTryReserveUserQuota(sender.Id, 80)
	require.NoError(t, err)
	require.Equal(t, cacheQuotaOK, reserved)

	quote := createWalletTransferTestQuote(t, identity, recipient.Id, 50, 0)
	failed = false
	_, err = ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
	})
	require.True(t, failed, "the reservation timed out")
	assert.ErrorIs(t, err, ErrWalletTransferBalanceUnverified)
	cached, err := getUserQuotaCache(sender.Id)
	require.NoError(t, err)
	assert.Equal(t, 20, cached, "the running request still holds its 80")

	retry := createWalletTransferTestQuote(t, identity, recipient.Id, 50, 0)
	_, err = ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: retry.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
	})
	assert.ErrorIs(t, err, ErrWalletTransferInsufficient)
	assert.Equal(t, 100, getUserQuotaFromDB(t, sender.Id))
	assert.Equal(t, 0, getUserQuotaFromDB(t, recipient.Id))
}

// When the reservation ran but its answer was lost, exactly that amount is
// given back to the cache.
func TestWalletTransferTimeoutAfterReservingGivesThatAmountBack(t *testing.T) {
	prepareWalletTransferTest(t)
	failed := false
	useWalletTransferTestRedis(t, failReservationOnce{afterRun: true, failed: &failed})
	sender, identity, loginPassword, paymentPassword := createWalletTransferTestAccount(t, "lost-answer-sender", 100, true)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "lost-answer-recipient", 0, false)
	_, err := GetUserCache(sender.Id)
	require.NoError(t, err)

	quote := createWalletTransferTestQuote(t, identity, recipient.Id, 50, 0)
	_, err = ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
	})
	require.True(t, failed, "the reservation ran and its answer was lost")
	assert.ErrorIs(t, err, ErrWalletTransferBalanceUnverified)
	cached, err := getUserQuotaCache(sender.Id)
	require.NoError(t, err)
	assert.Equal(t, 100, cached)

	retry := createWalletTransferTestQuote(t, identity, recipient.Id, 50, 0)
	_, err = ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: retry.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
	})
	require.NoError(t, err)
	assert.Equal(t, 50, getUserQuotaFromDB(t, sender.Id))
	cached, err = getUserQuotaCache(sender.Id)
	require.NoError(t, err)
	assert.Equal(t, 50, cached)
}

// The Redis client resends a command whose answer was lost, with the same
// key. The resent reservation must not take the amount a second time.
func TestWalletTransferReservationResentWithTheSameKeyTakesTheAmountOnce(t *testing.T) {
	prepareWalletTransferTest(t)
	failed := true
	useWalletTransferTestRedis(t, failScriptOnce{failed: &failed})
	sender, _, _, _ := createWalletTransferTestAccount(t, "resend-sender", 100, false)
	_, err := GetUserCache(sender.Id)
	require.NoError(t, err)
	key := walletTransferReservationKey(1)

	for range 2 {
		result, err := reserveWalletTransferQuota(sender.Id, key, 50)
		require.NoError(t, err)
		assert.Equal(t, cacheQuotaOK, result)
	}

	cached, err := getUserQuotaCache(sender.Id)
	require.NoError(t, err)
	assert.Equal(t, 50, cached)
}

// commitFailingPool makes the next transaction commit report an error. With
// committed set, the commit really happens first, as when its answer is lost.
// Setting *failQuery makes the next transaction fail to start, as when the
// database cannot be reached.
type commitFailingPool struct {
	*sql.DB
	failCommit *bool
	committed  bool
	failQuery  *bool
}

func (pool commitFailingPool) BeginTx(ctx context.Context, opts *sql.TxOptions) (gorm.ConnPool, error) {
	if *pool.failQuery {
		*pool.failQuery = false
		return nil, errors.New("connection reset")
	}
	tx, err := pool.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &commitFailingTx{Tx: tx, pool: pool}, nil
}

type commitFailingTx struct {
	*sql.Tx
	pool commitFailingPool
}

func (tx *commitFailingTx) Commit() error {
	if !*tx.pool.failCommit {
		return tx.Tx.Commit()
	}
	*tx.pool.failCommit = false
	if tx.pool.committed {
		if err := tx.Tx.Commit(); err != nil {
			return err
		}
	} else {
		_ = tx.Tx.Rollback()
	}
	return errors.New("connection reset during commit")
}

func useCommitFailingDB(t *testing.T, pool commitFailingPool) {
	t.Helper()
	sqlDB, err := DB.DB()
	require.NoError(t, err)
	pool.DB = sqlDB
	failing, err := gorm.Open(&sqlite.Dialector{Conn: pool}, &gorm.Config{})
	require.NoError(t, err)
	previous := DB
	DB = failing
	t.Cleanup(func() { DB = previous })
}

func walletTransferFlowId(t *testing.T, quote *WalletTransferQuoteResult) int64 {
	t.Helper()
	var flow AuthFlow
	require.NoError(t, DB.Where("token_hash = ?", authFlowTokenHash(quote.QuoteToken)).First(&flow).Error)
	return flow.Id
}

// prepareUnknownCommit sets up usage of 20 waiting in the batch queue and a
// review of 50 whose commit will report an error.
func prepareUnknownCommit(t *testing.T, committed bool) (User, User, *WalletTransferQuoteResult, WalletTransferSubmit, AuthSessionIdentity, *bool, *bool) {
	t.Helper()
	prepareWalletTransferTest(t)
	oldBatch, oldDelay := common.BatchUpdateEnabled, walletTransferResolveDelay
	common.BatchUpdateEnabled = true
	// Only the test decides parked executions.
	walletTransferResolveDelay = func(int) time.Duration { return time.Hour }
	t.Cleanup(func() {
		common.BatchUpdateEnabled, walletTransferResolveDelay = oldBatch, oldDelay
		uncertainWalletTransfers.Clear()
	})
	sender, identity, loginPassword, paymentPassword := createWalletTransferTestAccount(t, "unknown-commit-sender", 100, true)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "unknown-commit-recipient", 0, false)
	failCommit, failQuery := new(bool), new(bool)
	useCommitFailingDB(t, commitFailingPool{failCommit: failCommit, committed: committed, failQuery: failQuery})
	quote := createWalletTransferTestQuote(t, identity, recipient.Id, 50, 0)
	addNewRecord(BatchUpdateTypeUserQuota, sender.Id, -20)
	*failCommit = true
	submit := WalletTransferSubmit{QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword}
	return sender, recipient, quote, submit, identity, failCommit, failQuery
}

// The commit happened but its answer was lost. The queued usage of 20 must be
// charged once: 100 - 20 - 50 = 30.
func TestWalletTransferWithAnUnknownCommitChargesQueuedUsageOnce(t *testing.T) {
	sender, recipient, quote, submit, identity, _, failQuery := prepareUnknownCommit(t, true)

	_, err := ConfirmWalletTransfer(identity, submit)
	assert.ErrorIs(t, err, ErrWalletTransferOutcomeUnknown)
	batchUpdate()
	assert.Equal(t, 30, getUserQuotaFromDB(t, sender.Id))
	assert.Equal(t, 50, getUserQuotaFromDB(t, recipient.Id))
	assert.Equal(t, int64(-20), parkedWalletTransferDeltas(sender.Id), "the usage waits while undecided")

	// While the database cannot be read, the execution stays parked.
	flowId := walletTransferFlowId(t, quote)
	*failQuery = true
	require.Error(t, resolveUncertainWalletTransfer(flowId))
	assert.Equal(t, int64(-20), parkedWalletTransferDeltas(sender.Id))

	require.NoError(t, resolveUncertainWalletTransfer(flowId))
	assert.Zero(t, parkedWalletTransferDeltas(sender.Id))
	batchUpdate()
	assert.Equal(t, 30, getUserQuotaFromDB(t, sender.Id))

	// Checking the result with the same review returns the finished transfer.
	transfer, err := ConfirmWalletTransfer(identity, submit)
	require.NoError(t, err)
	assert.Equal(t, int64(50), transfer.Amount)
	batchUpdate()
	assert.Equal(t, 30, getUserQuotaFromDB(t, sender.Id))
}

// The commit did not happen. Checking with the same review gives the parked
// usage back to the queue and carries the transfer out once.
func TestWalletTransferWithAFailedCommitIsCarriedOutWhenChecked(t *testing.T) {
	sender, recipient, _, submit, identity, _, _ := prepareUnknownCommit(t, false)

	_, err := ConfirmWalletTransfer(identity, submit)
	assert.ErrorIs(t, err, ErrWalletTransferOutcomeUnknown)
	batchUpdate()
	assert.Equal(t, 100, getUserQuotaFromDB(t, sender.Id), "nothing was written and the usage waits")
	assert.Equal(t, 0, getUserQuotaFromDB(t, recipient.Id))

	_, err = ConfirmWalletTransfer(identity, submit)
	require.NoError(t, err)
	batchUpdate()
	assert.Zero(t, parkedWalletTransferDeltas(sender.Id))
	assert.Equal(t, 30, getUserQuotaFromDB(t, sender.Id))
	assert.Equal(t, 50, getUserQuotaFromDB(t, recipient.Id))
}

func parkedWalletTransferDeltas(userId int) int64 {
	var total int64
	uncertainWalletTransfers.Range(func(_, value any) bool {
		total += int64(value.(*uncertainWalletTransfer).pending[userId])
		return true
	})
	return total
}

// A second transfer while the first one is undecided must not count the
// first one's parked usage into the balance it writes: that usage is either in
// the database already or goes back to the queue.
func TestWalletTransferWhileAnotherIsUndecidedChargesUsageOnce(t *testing.T) {
	for _, tc := range []struct {
		name      string
		committed bool
		sender    int
		recipient int
	}{
		{"first committed", true, 100 - 20 - 50 - 10, 60},
		{"first rolled back", false, 100 - 20 - 10, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sender, recipient, first, submit, identity, _, _ := prepareUnknownCommit(t, tc.committed)
			_, err := ConfirmWalletTransfer(identity, submit)
			require.ErrorIs(t, err, ErrWalletTransferOutcomeUnknown)

			second := createWalletTransferTestQuote(t, identity, recipient.Id, 10, 0)
			_, err = ConfirmWalletTransfer(identity, WalletTransferSubmit{
				QuoteToken: second.QuoteToken, LoginPassword: submit.LoginPassword, PaymentPassword: submit.PaymentPassword,
			})
			require.NoError(t, err)
			assert.Zero(t, parkedWalletTransferDeltas(sender.Id), "the first transfer was decided before")
			// A late background check finds nothing left to do.
			require.NoError(t, resolveUncertainWalletTransfer(walletTransferFlowId(t, first)))
			batchUpdate()

			assert.Equal(t, tc.sender, getUserQuotaFromDB(t, sender.Id))
			assert.Equal(t, tc.recipient, getUserQuotaFromDB(t, recipient.Id))
		})
	}
}

// While an undecided transfer cannot be decided, new transfers wait and write
// nothing.
func TestWalletTransferWaitsWhileAPreviousOneCannotBeDecided(t *testing.T) {
	sender, recipient, _, submit, identity, _, unreachable := prepareUnknownCommit(t, true)
	_, err := ConfirmWalletTransfer(identity, submit)
	require.ErrorIs(t, err, ErrWalletTransferOutcomeUnknown)
	second := createWalletTransferTestQuote(t, identity, recipient.Id, 10, 0)
	secondSubmit := WalletTransferSubmit{QuoteToken: second.QuoteToken, LoginPassword: submit.LoginPassword, PaymentPassword: submit.PaymentPassword}

	*unreachable = true
	_, err = ConfirmWalletTransfer(identity, secondSubmit)
	assert.ErrorIs(t, err, ErrWalletTransferPreviousPending)
	assert.Equal(t, 30, getUserQuotaFromDB(t, sender.Id))
	assert.Equal(t, 50, getUserQuotaFromDB(t, recipient.Id))

	_, err = ConfirmWalletTransfer(identity, secondSubmit)
	require.NoError(t, err)
	batchUpdate()
	assert.Equal(t, 20, getUserQuotaFromDB(t, sender.Id))
	assert.Equal(t, 60, getUserQuotaFromDB(t, recipient.Id))
}

// A graceful shutdown decides undecided transfers and writes the batch queue,
// so the usage of a rolled-back one is not lost.
func TestWalletTransferSettlesBeforeShutdown(t *testing.T) {
	sender, recipient, _, submit, identity, _, _ := prepareUnknownCommit(t, false)
	_, err := ConfirmWalletTransfer(identity, submit)
	require.ErrorIs(t, err, ErrWalletTransferOutcomeUnknown)

	SettlePendingQuotaBeforeShutdown()

	assert.Zero(t, parkedWalletTransferDeltas(sender.Id))
	assert.Equal(t, 80, getUserQuotaFromDB(t, sender.Id))
	assert.Equal(t, 0, getUserQuotaFromDB(t, recipient.Id))
}

// The passwords are hashed before the transaction; a transfer still uses the
// stored passwords as they are when it runs.
func TestWalletTransferChecksThePasswordsStoredWhenItRuns(t *testing.T) {
	prepareWalletTransferTest(t)
	sender, identity, loginPassword, paymentPassword := createWalletTransferTestAccount(t, "rehash-sender", 100, true)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "rehash-recipient", 0, false)
	newPaymentHash, err := common.HashAccountPassword("a brand new payment password")
	require.NoError(t, err)
	require.NoError(t, DB.Model(&WalletSecurity{}).Where("user_id = ?", sender.Id).Update("password_hash", newPaymentHash).Error)

	quote := createWalletTransferTestQuote(t, identity, recipient.Id, 10, 0)
	_, err = ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
	})
	assert.ErrorIs(t, err, ErrWalletTransferPasswordInvalid, "the old payment password no longer works")

	quote = createWalletTransferTestQuote(t, identity, recipient.Id, 10, 0)
	_, err = ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: "a brand new payment password",
	})
	require.NoError(t, err)
	assert.Equal(t, 90, getUserQuotaFromDB(t, sender.Id))
}

// Balances may exceed the single-request limit (math.MaxInt32); only the
// wallet limit applies to them.
func TestWalletTransferWorksForBalancesAboveTheRequestLimit(t *testing.T) {
	prepareWalletTransferTest(t)
	sender, identity, loginPassword, paymentPassword := createWalletTransferTestAccount(t, "rich-sender", 0, true)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "rich-recipient", 0, false)
	large := int64(common.MaxQuota) + 1_000
	require.NoError(t, DB.Model(&User{}).Where("id = ?", sender.Id).Update("quota", large).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", recipient.Id).Update("quota", large).Error)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", sender.Id).Update("role", common.RoleRootUser).Error)

	quote := createWalletTransferTestQuote(t, identity, recipient.Id, 10, 0)
	_, err := ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
	})
	require.NoError(t, err)
	assert.Equal(t, int(large-10), getUserQuotaFromDB(t, sender.Id))
	assert.Equal(t, int(large+10), getUserQuotaFromDB(t, recipient.Id))
}

func TestWalletTransferFeeMustLeaveTheRecipientSomething(t *testing.T) {
	for fee, valid := range map[float64]bool{0: true, 1.5: true, 99.99: true, 100: false, -1: false, 1.234: false} {
		assert.Equal(t, valid, operation_setting.ValidateWalletTransferFeePercent(fee), "fee %v", fee)
	}
}

func TestWalletTransferDailyLimitIsHundredDisplayUnits(t *testing.T) {
	for _, tc := range []struct {
		displayType string
		rate        float64
		want        int64
	}{
		{"USD", 1, int64(100 * common.QuotaPerUnit)},
		{"CNY", 8, int64(100 * common.QuotaPerUnit / 8)},
		// Token display has no currency, so the cap is 100 USD worth of quota.
		{"TOKENS", 1, int64(100 * common.QuotaPerUnit)},
	} {
		got, err := checkedWalletTransferLimit(tc.rate, tc.displayType)
		require.NoError(t, err)
		assert.Equal(t, tc.want, got, tc.displayType)
	}
}

func TestWalletTransferLocksWrongPasswordAndUnlocksAfterTimeout(t *testing.T) {
	prepareWalletTransferTest(t)
	sender, identity, loginPassword, _ := createWalletTransferTestAccount(t, "locked-sender", 100, true)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "locked-recipient", 0, false)
	// A wrong login password and a wrong payment password fail identically.
	wrongLoginQuote := createWalletTransferTestQuote(t, identity, recipient.Id, 10, 0)
	_, wrongLoginErr := ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: wrongLoginQuote.QuoteToken, LoginPassword: "incorrect password", PaymentPassword: "secure payment password",
	})
	assert.ErrorIs(t, wrongLoginErr, ErrWalletTransferPasswordInvalid)
	// A used review cannot be submitted again.
	_, replayErr := ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: wrongLoginQuote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: "secure payment password",
	})
	assert.ErrorIs(t, replayErr, ErrWalletTransferQuoteInvalid)
	require.NoError(t, DB.Model(&WalletSecurity{}).Where("user_id = ?", sender.Id).Update("failed_attempts", 0).Error)
	for range common.MaxFailAttempts {
		quote := createWalletTransferTestQuote(t, identity, recipient.Id, 10, 0)
		_, err := ConfirmWalletTransfer(identity, WalletTransferSubmit{
			QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: "incorrect password",
		})
		assert.ErrorIs(t, err, ErrWalletTransferPasswordInvalid)
	}
	lockedQuote := createWalletTransferTestQuote(t, identity, recipient.Id, 10, 0)
	_, err := ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: lockedQuote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: "incorrect password",
	})
	assert.ErrorIs(t, err, ErrWalletTransferPasswordLocked)

	require.NoError(t, DB.Model(&WalletSecurity{}).Where("user_id = ?", sender.Id).Updates(map[string]any{
		"locked_until": time.Now().Add(-time.Second),
	}).Error)
	quote := createWalletTransferTestQuote(t, identity, recipient.Id, 10, 0)
	transfer, err := ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: "secure payment password",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(10), transfer.Received)
	assert.Equal(t, 90, getUserQuotaFromDB(t, sender.Id))
	assert.Equal(t, 10, getUserQuotaFromDB(t, recipient.Id))
}

func TestAdminWalletTransferFiltersApplyToBothSidesAndDate(t *testing.T) {
	prepareWalletTransferTest(t)
	sender, _, _, _ := createWalletTransferTestAccount(t, "history-sender", 100, false)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "history-recipient", 0, false)
	createdAt := time.Now()
	require.NoError(t, DB.Create(&WalletTransfer{
		QuoteFlowId: 700003, SenderId: sender.Id, SenderUsername: sender.Username,
		RecipientId: recipient.Id, RecipientUsername: recipient.Username,
		Amount: 10, Fee: 1, Received: 9, FeePercent: "10.00", DateKey: "2026-01-01", CreatedAt: createdAt,
	}).Error)
	_, total, fees, err := SearchWalletTransfers(sender.Id, "", "2026-01-02", 0, 20)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.Zero(t, fees)
	records, total, fees, err := SearchWalletTransfers(recipient.Id, sender.Username, "2026-01-01", 0, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, int64(1), fees)
	require.Len(t, records, 1)
	assert.Equal(t, int64(9), records[0].Received)
}

func TestWalletTransferRejectsInsufficientBalanceAndPrunesOnlyShortLivedState(t *testing.T) {
	prepareWalletTransferTest(t)
	sender, identity, loginPassword, paymentPassword := createWalletTransferTestAccount(t, "poor-sender", 5, true)
	recipient, _, _, _ := createWalletTransferTestAccount(t, "poor-recipient", 0, false)
	quote := createWalletTransferTestQuote(t, identity, recipient.Id, 10, 0)
	_, err := ConfirmWalletTransfer(identity, WalletTransferSubmit{
		QuoteToken: quote.QuoteToken, LoginPassword: loginPassword, PaymentPassword: paymentPassword,
	})
	assert.ErrorIs(t, err, ErrWalletTransferInsufficient)
	assert.Equal(t, 5, getUserQuotaFromDB(t, sender.Id))
	assert.Equal(t, 0, getUserQuotaFromDB(t, recipient.Id))
	var transferCount int64
	require.NoError(t, DB.Model(&WalletTransfer{}).Count(&transferCount).Error)
	assert.Zero(t, transferCount)

	now := time.Now()
	oldDay := walletTransferDateKey(now.AddDate(0, 0, -WalletTransferStateRetentionDays-1))
	currentDay := walletTransferDateKey(now)
	require.NoError(t, DB.Create(&WalletTransferAttempt{QuoteFlowId: 800001, UserId: sender.Id, DateKey: oldDay, Quota: 1, SubmittedAt: now}).Error)
	require.NoError(t, DB.Create(&WalletTransferAttempt{QuoteFlowId: 800002, UserId: sender.Id, DateKey: currentDay, Quota: 1, SubmittedAt: now}).Error)
	require.NoError(t, DB.Create(&WalletTransferDailyConfig{UserId: sender.Id, DateKey: oldDay, DisplayType: "USD", DisplaySymbol: "$", DailyLimitQuota: 1}).Error)
	require.NoError(t, DB.Create(&WalletTransfer{QuoteFlowId: 900001, SenderId: sender.Id, SenderUsername: sender.Username, RecipientId: recipient.Id, RecipientUsername: recipient.Username, Amount: 1, Received: 1, FeePercent: "0.00", DateKey: oldDay, CreatedAt: now}).Error)
	require.NoError(t, DeleteExpiredWalletTransferState(now))
	var oldAttempts, recentAttempts, oldConfigs, recentConfigs, retainedTransfers int64
	require.NoError(t, DB.Model(&WalletTransferAttempt{}).Where("date_key = ?", oldDay).Count(&oldAttempts).Error)
	require.NoError(t, DB.Model(&WalletTransferAttempt{}).Where("date_key = ?", currentDay).Count(&recentAttempts).Error)
	require.NoError(t, DB.Model(&WalletTransferDailyConfig{}).Where("date_key = ?", oldDay).Count(&oldConfigs).Error)
	require.NoError(t, DB.Model(&WalletTransferDailyConfig{}).Where("date_key = ?", currentDay).Count(&recentConfigs).Error)
	require.NoError(t, DB.Model(&WalletTransfer{}).Where("date_key = ?", oldDay).Count(&retainedTransfers).Error)
	assert.Zero(t, oldAttempts)
	assert.EqualValues(t, 2, recentAttempts)
	assert.Zero(t, oldConfigs)
	assert.EqualValues(t, 1, recentConfigs)
	assert.EqualValues(t, 1, retainedTransfers, "reconciliation history is permanent")
}

func TestWalletTransferPaymentPasswordFlowRequiresEmailCodeAndIsSingleUse(t *testing.T) {
	prepareWalletTransferTest(t)
	_, identity, loginPassword, _ := createWalletTransferTestAccount(t, "password-flow-setter", 0, false)
	require.NoError(t, DB.AutoMigrate(&AuthFlow{}, &User{}, &UserSession{}))
	codeHash := mustWalletPasswordHash(t, "482910")
	token, expiresAt, email, err := CreateWalletTransferPasswordFlow(identity, "set", codeHash)
	require.NoError(t, err)
	assert.True(t, expiresAt.After(time.Now()))
	assert.Equal(t, "password-flow-setter@example.com", email)

	newPassword := "separate wallet password"
	newPasswordHash, err := common.HashAccountPassword(newPassword)
	require.NoError(t, err)
	err = CompleteWalletTransferPasswordFlow(identity, token, "invalid-code", "", "", "", newPassword, newPasswordHash)
	assert.ErrorIs(t, err, ErrWalletPasswordCodeInvalid)

	rotatedCodeHash := mustWalletPasswordHash(t, "119385")
	rotatedToken, _, _, err := CreateWalletTransferPasswordFlow(identity, "set", rotatedCodeHash)
	require.NoError(t, err)
	err = CompleteWalletTransferPasswordFlow(identity, token, "482910", "", "", "", newPassword, newPasswordHash)
	assert.ErrorIs(t, err, ErrWalletPasswordFlowInvalid, "a newer code invalidates the previous flow")

	// A password from the common-password list is refused in any letter case.
	err = CompleteWalletTransferPasswordFlow(identity, rotatedToken, "119385", "", loginPassword, "", "Password1", newPasswordHash)
	assert.ErrorIs(t, err, common.ErrPasswordTooCommon)
	// The email code alone is not enough: the login password is required too.
	err = CompleteWalletTransferPasswordFlow(identity, rotatedToken, "119385", "", "", "", newPassword, newPasswordHash)
	assert.ErrorIs(t, err, ErrWalletTransferLoginInvalid)
	var created int64
	require.NoError(t, DB.Model(&WalletSecurity{}).Where("user_id = ?", identity.UserID).Count(&created).Error)
	assert.Zero(t, created)

	err = CompleteWalletTransferPasswordFlow(identity, rotatedToken, "119385", "", loginPassword, "", newPassword, newPasswordHash)
	require.NoError(t, err)
	var security WalletSecurity
	require.NoError(t, DB.Where("user_id = ?", identity.UserID).First(&security).Error)
	assert.True(t, common.ValidatePasswordAndHash(newPassword, security.PasswordHash))

	err = CompleteWalletTransferPasswordFlow(identity, rotatedToken, "119385", "", loginPassword, "", newPassword, newPasswordHash)
	assert.ErrorIs(t, err, ErrWalletPasswordFlowInvalid)
}

func TestWalletTransferPaymentPasswordEmailIsWaivedOnlyForRoot(t *testing.T) {
	prepareWalletTransferTest(t)
	user, identity, loginPassword, _ := createWalletTransferTestAccount(t, "password-no-email", 0, false)
	require.NoError(t, DB.AutoMigrate(&AuthFlow{}, &User{}, &UserSession{}))
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("email", "").Error)
	codeHash := mustWalletPasswordHash(t, "271828")
	newPassword := "root wallet passphrase"
	newPasswordHash, err := common.HashAccountPassword(newPassword)
	require.NoError(t, err)

	_, _, _, err = CreateWalletTransferPasswordFlow(identity, "set", codeHash)
	assert.ErrorIs(t, err, ErrEmailBindingRequired, "an ordinary user must bind an email")

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("role", common.RoleRootUser).Error)
	token, _, email, err := CreateWalletTransferPasswordFlow(identity, "set", codeHash)
	require.NoError(t, err)
	assert.Empty(t, email)
	status, err := GetWalletTransferStatus(user.Id, 1_000, "USD", "$ ")
	require.NoError(t, err)
	assert.True(t, status.EmailExempt)

	// The login password is still required, and the waiver ends with the root role.
	require.NoError(t, ValidateWalletTransferPasswordCode(identity, token, ""))
	err = CompleteWalletTransferPasswordFlow(identity, token, "", "", "wrong login password", "", newPassword, newPasswordHash)
	assert.ErrorIs(t, err, ErrWalletTransferLoginInvalid)
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("role", common.RoleAdminUser).Error)
	err = CompleteWalletTransferPasswordFlow(identity, token, "", "", loginPassword, "", newPassword, newPasswordHash)
	assert.ErrorIs(t, err, ErrEmailBindingRequired)

	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("role", common.RoleRootUser).Error)
	require.NoError(t, CompleteWalletTransferPasswordFlow(identity, token, "", "", loginPassword, "", newPassword, newPasswordHash))
	var security WalletSecurity
	require.NoError(t, DB.Where("user_id = ?", user.Id).First(&security).Error)
	assert.True(t, common.ValidatePasswordAndHash(newPassword, security.PasswordHash))

	// A bound email does not bring the email step back for the root user: no
	// address is returned, so no code is sent, and the current password still counts.
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("email", "root@example.com").Error)
	changeToken, _, changeEmail, err := CreateWalletTransferPasswordFlow(identity, "change", codeHash)
	require.NoError(t, err)
	assert.Empty(t, changeEmail)
	changedPassword := "another root passphrase"
	changedHash, err := common.HashAccountPassword(changedPassword)
	require.NoError(t, err)
	err = CompleteWalletTransferPasswordFlow(identity, changeToken, "", "not the current password", "", "", changedPassword, changedHash)
	assert.ErrorIs(t, err, ErrWalletPasswordCurrentInvalid)
	require.NoError(t, CompleteWalletTransferPasswordFlow(identity, changeToken, "", newPassword, "", "", changedPassword, changedHash))

	// An ordinary user with a bound email still gets a code to that address.
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Update("role", common.RoleCommonUser).Error)
	_, _, ordinaryEmail, err := CreateWalletTransferPasswordFlow(identity, "change", codeHash)
	require.NoError(t, err)
	assert.Equal(t, "root@example.com", ordinaryEmail)
}

func TestWalletTransferPaymentPasswordMustDifferFromLoginAndCurrentPassword(t *testing.T) {
	prepareWalletTransferTest(t)
	sender, identity, loginPassword, currentPaymentPassword := createWalletTransferTestAccount(t, "password-separation", 0, true)
	require.NoError(t, DB.AutoMigrate(&AuthFlow{}, &User{}, &UserSession{}))
	codeHash := mustWalletPasswordHash(t, "513792")
	token, _, _, err := CreateWalletTransferPasswordFlow(identity, "change", codeHash)
	require.NoError(t, err)

	loginPasswordHash, err := common.HashAccountPassword(loginPassword)
	require.NoError(t, err)
	// Without the current payment password, the answer never reveals whether the
	// proposed password equals the login or the current payment password.
	for _, guess := range []string{loginPassword, currentPaymentPassword} {
		err = CompleteWalletTransferPasswordFlow(identity, token, "513792", "not the current password", "", "", guess, loginPasswordHash)
		assert.ErrorIs(t, err, ErrWalletPasswordCurrentInvalid)
	}
	err = CompleteWalletTransferPasswordFlow(identity, token, "513792", currentPaymentPassword, "", "", loginPassword, loginPasswordHash)
	assert.ErrorIs(t, err, ErrWalletPasswordSameAsLogin)

	currentPaymentHash, err := common.HashAccountPassword(currentPaymentPassword)
	require.NoError(t, err)
	err = CompleteWalletTransferPasswordFlow(identity, token, "513792", currentPaymentPassword, "", "", currentPaymentPassword, currentPaymentHash)
	assert.ErrorIs(t, err, ErrWalletPasswordUnchanged)

	newPassword := "another wallet passphrase"
	newPasswordHash, err := common.HashAccountPassword(newPassword)
	require.NoError(t, err)
	err = CompleteWalletTransferPasswordFlow(identity, token, "513792", currentPaymentPassword, "", "", newPassword, newPasswordHash)
	require.NoError(t, err)
	var security WalletSecurity
	require.NoError(t, DB.Where("user_id = ?", sender.Id).First(&security).Error)
	assert.True(t, common.ValidatePasswordAndHash(newPassword, security.PasswordHash))
}

func TestWalletTransferPasswordChangeLockCannotBeBypassedWithNewEmailFlow(t *testing.T) {
	prepareWalletTransferTest(t)
	sender, identity, _, currentPassword := createWalletTransferTestAccount(t, "password-change-lock", 0, true)
	require.NoError(t, DB.AutoMigrate(&AuthFlow{}, &User{}, &UserSession{}))
	newPassword := "new payment password after lock"
	newPasswordHash, err := common.HashAccountPassword(newPassword)
	require.NoError(t, err)

	for attempt := range common.MaxFailAttempts {
		codeHash := mustWalletPasswordHash(t, "482910")
		token, _, _, flowErr := CreateWalletTransferPasswordFlow(identity, "change", codeHash)
		require.NoError(t, flowErr)
		err = CompleteWalletTransferPasswordFlow(identity, token, "482910", "wrong current password", "", "", newPassword, newPasswordHash)
		assert.ErrorIs(t, err, ErrWalletPasswordCurrentInvalid, "each flow reports the failed current password")
		if attempt+1 == common.MaxFailAttempts {
			var security WalletSecurity
			require.NoError(t, DB.Where("user_id = ?", sender.Id).First(&security).Error)
			assert.NotNil(t, security.LockedUntil)
		}
	}

	codeHash := mustWalletPasswordHash(t, "713064")
	token, _, _, err := CreateWalletTransferPasswordFlow(identity, "change", codeHash)
	require.NoError(t, err)
	err = CompleteWalletTransferPasswordFlow(identity, token, "713064", currentPassword, "", "", newPassword, newPasswordHash)
	assert.ErrorIs(t, err, ErrWalletTransferPasswordLocked, "a fresh email flow must not bypass the account lock")

	var security WalletSecurity
	require.NoError(t, DB.Where("user_id = ?", sender.Id).First(&security).Error)
	assert.True(t, common.ValidatePasswordAndHash(currentPassword, security.PasswordHash))
	assert.False(t, common.ValidatePasswordAndHash(newPassword, security.PasswordHash))
}

func TestWalletTransferPasswordCodeExpiresAndLocksAfterRepeatedFailures(t *testing.T) {
	prepareWalletTransferTest(t)
	_, identity, _, _ := createWalletTransferTestAccount(t, "password-code-guard", 0, false)
	require.NoError(t, DB.AutoMigrate(&AuthFlow{}, &User{}, &UserSession{}))
	codeHash := mustWalletPasswordHash(t, "628194")
	token, _, _, err := CreateWalletTransferPasswordFlow(identity, "set", codeHash)
	require.NoError(t, err)
	newPassword := "a different secure password"

	for attempt := range WalletPasswordMaxAttempts {
		err = CompleteWalletTransferPasswordFlow(identity, token, "invalid-code", "", "", "", newPassword, "unused-hash")
		if attempt+1 == WalletPasswordMaxAttempts {
			assert.ErrorIs(t, err, ErrWalletPasswordFlowLocked)
			continue
		}
		assert.ErrorIs(t, err, ErrWalletPasswordCodeInvalid)
	}
	err = CompleteWalletTransferPasswordFlow(identity, token, "628194", "", "", "", newPassword, "unused-hash")
	assert.ErrorIs(t, err, ErrWalletPasswordFlowLocked, "a locked flow cannot be recovered by supplying the right code")

	expiredToken, _, _, err := CreateWalletTransferPasswordFlow(identity, "set", codeHash)
	require.NoError(t, err)
	flowHash := authFlowTokenHash(expiredToken)
	require.NoError(t, DB.Model(&AuthFlow{}).Where("token_hash = ?", flowHash).Update("expires_at", time.Now().Add(-time.Minute)).Error)
	err = CompleteWalletTransferPasswordFlow(identity, expiredToken, "628194", "", "", "", newPassword, "unused-hash")
	assert.ErrorIs(t, err, ErrWalletPasswordFlowInvalid)
}

func TestWalletTransferPaymentPasswordResetRequiresLoginPasswordAndFreshTwoFA(t *testing.T) {
	prepareWalletTransferTest(t)
	sender, identity, loginPassword, oldPaymentPassword := createWalletTransferTestAccount(t, "password-flow-resetter", 0, true)
	require.NoError(t, DB.AutoMigrate(&AuthFlow{}, &User{}, &UserSession{}, &TwoFA{}, &TwoFABackupCode{}))
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "New Utopia", AccountName: sender.Username})
	require.NoError(t, err)
	require.NoError(t, DB.Create(&TwoFA{UserId: sender.Id, Secret: key.Secret(), IsEnabled: true}).Error)

	codeHash := mustWalletPasswordHash(t, "734205")
	token, _, _, err := CreateWalletTransferPasswordFlow(identity, "reset", codeHash)
	require.NoError(t, err)
	newPassword := "reset wallet password safely"
	newPasswordHash, err := common.HashAccountPassword(newPassword)
	require.NoError(t, err)
	twoFACode, err := totp.GenerateCode(key.Secret(), time.Now())
	require.NoError(t, err)

	err = CompleteWalletTransferPasswordFlow(identity, token, "734205", "", "wrong login password", twoFACode, newPassword, newPasswordHash)
	assert.ErrorIs(t, err, ErrWalletTransferLoginInvalid)

	err = CompleteWalletTransferPasswordFlow(identity, token, "734205", "", loginPassword, "invalid-code", newPassword, newPasswordHash)
	assert.ErrorIs(t, err, ErrWalletTransferTwoFAInvalid)

	err = CompleteWalletTransferPasswordFlow(identity, token, "734205", "", loginPassword, twoFACode, newPassword, newPasswordHash)
	require.NoError(t, err)
	var security WalletSecurity
	require.NoError(t, DB.Where("user_id = ?", sender.Id).First(&security).Error)
	assert.True(t, common.ValidatePasswordAndHash(newPassword, security.PasswordHash))
	assert.False(t, common.ValidatePasswordAndHash(oldPaymentPassword, security.PasswordHash))

	err = CompleteWalletTransferPasswordFlow(identity, token, "734205", "", loginPassword, twoFACode, newPassword, newPasswordHash)
	assert.ErrorIs(t, err, ErrWalletPasswordFlowInvalid)
}

func mustWalletPasswordHash(t *testing.T, password string) string {
	t.Helper()
	hash, err := common.Password2Hash(password)
	require.NoError(t, err)
	return hash
}
