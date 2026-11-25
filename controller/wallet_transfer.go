package controller

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

type walletTransferQuoteRequest struct {
	RecipientId int   `json:"recipient_id"`
	Amount      int64 `json:"amount"`
}

type walletTransferConfirmRequest struct {
	QuoteToken      string `json:"quote_token"`
	LoginPassword   string `json:"login_password"`
	PaymentPassword string `json:"payment_password"`
	TwoFACode       string `json:"two_fa_code"`
}

type walletTransferPasswordStartRequest struct {
	Intent string `json:"intent"`
}

type walletTransferPasswordCompleteRequest struct {
	FlowToken       string `json:"flow_token"`
	Code            string `json:"code"`
	CurrentPassword string `json:"current_password"`
	LoginPassword   string `json:"login_password"`
	TwoFACode       string `json:"two_fa_code"`
	NewPassword     string `json:"new_password"`
}

func walletTransferIdentity(c *gin.Context) (model.AuthSessionIdentity, bool) {
	identity, ok := middleware.GetSessionAuthIdentity(c)
	if !ok {
		return model.AuthSessionIdentity{}, false
	}
	return identity, true
}

func GetWalletTransferStatus(c *gin.Context) {
	snapshot, err := model.GetWalletTransferDailyLimitSnapshot()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	status, err := model.GetWalletTransferStatus(c.GetInt("id"), snapshot.Quota, snapshot.DisplayType, snapshot.Symbol)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	status.FeePercent = strconv.FormatFloat(operation_setting.GetPaymentSetting().WalletTransferFeePercent, 'f', 2, 64)
	common.ApiSuccess(c, status)
}

func SearchWalletTransferRecipients(c *gin.Context) {
	recipients, err := model.SearchWalletTransferRecipients(c.GetInt("id"), c.Query("keyword"))
	if err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, recipients)
}

func CreateWalletTransferQuote(c *gin.Context) {
	identity, ok := walletTransferIdentity(c)
	if !ok {
		common.ApiErrorStatus(c, http.StatusUnauthorized, common.NewMessage("A signed-in browser session is required."))
		return
	}
	var request walletTransferQuoteRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	snapshot, err := model.GetWalletTransferDailyLimitSnapshot()
	if err != nil {
		common.ApiError(c, err)
		return
	}
	result, err := model.CreateWalletTransferQuote(
		identity, request.RecipientId, request.Amount,
		operation_setting.GetPaymentSetting().WalletTransferFeePercent,
		snapshot.Quota, snapshot.Amount, snapshot.DisplayType, snapshot.Symbol,
	)
	if err != nil {
		writeWalletTransferError(c, err)
		return
	}
	common.ApiSuccess(c, result)
}

func ConfirmWalletTransfer(c *gin.Context) {
	identity, ok := walletTransferIdentity(c)
	if !ok {
		common.ApiErrorStatus(c, http.StatusUnauthorized, common.NewMessage("A signed-in browser session is required."))
		return
	}
	var request walletTransferConfirmRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	transfer, err := model.ConfirmWalletTransfer(identity, model.WalletTransferSubmit{
		QuoteToken: request.QuoteToken, LoginPassword: request.LoginPassword,
		PaymentPassword: request.PaymentPassword, TwoFACode: request.TwoFACode,
	})
	params := map[string]any{"success": err == nil}
	if transfer != nil {
		params["transfer_id"] = transfer.Id
		params["recipient_id"] = transfer.RecipientId
		params["amount"] = transfer.Amount
		params["fee"] = transfer.Fee
		params["received"] = transfer.Received
	}
	recordUserSecurityAudit(c, identity.UserID, "user.wallet_transfer", params)
	if err != nil {
		writeWalletTransferError(c, err)
		return
	}
	common.ApiSuccess(c, transfer)
}

func GetWalletTransferHistory(c *gin.Context) {
	page := common.GetPageQuery(c)
	records, total, err := model.GetWalletTransfersForUser(c.GetInt("id"), c.Query("trade_no"), page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(records)
	common.ApiSuccess(c, page)
}

func StartWalletTransferPasswordFlow(c *gin.Context) {
	identity, ok := walletTransferIdentity(c)
	if !ok {
		common.ApiErrorStatus(c, http.StatusUnauthorized, common.NewMessage("A signed-in browser session is required."))
		return
	}
	var request walletTransferPasswordStartRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	flow, err := service.StartWalletTransferPasswordFlow(identity, request.Intent, service.AccountMailLanguage(c, c.GetInt("id")))
	succeeded := err == nil
	defer func() {
		recordUserSecurityAudit(c, identity.UserID, "user.wallet_payment_password_start", map[string]any{"intent": request.Intent, "success": succeeded})
	}()
	if err != nil {
		writeWalletTransferError(c, err)
		return
	}
	common.ApiSuccess(c, flow)
}

func CompleteWalletTransferPasswordFlow(c *gin.Context) {
	identity, ok := walletTransferIdentity(c)
	if !ok {
		common.ApiErrorStatus(c, http.StatusUnauthorized, common.NewMessage("A signed-in browser session is required."))
		return
	}
	var request walletTransferPasswordCompleteRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid parameters"))
		return
	}
	err := service.CompleteWalletTransferPasswordFlow(identity, request.FlowToken, request.Code, request.CurrentPassword, request.LoginPassword, request.TwoFACode, request.NewPassword)
	notificationFailed := false
	if err == nil {
		user, userErr := model.GetUserById(identity.UserID, false)
		if userErr != nil {
			notificationFailed = true
		} else if user.Email != "" && service.NotifyAccountSecurityChange(service.AccountMailLanguage(c, identity.UserID), user.Email, i18n.MsgSecurityEventPasswordUpdated) != nil {
			notificationFailed = true
		}
	}
	recordUserSecurityAudit(c, identity.UserID, "user.wallet_payment_password", map[string]any{"success": err == nil, "notification_failed": notificationFailed})
	if err != nil {
		writeWalletTransferError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{"success": true})
}

func GetAdminWalletTransfers(c *gin.Context) {
	userId, err := parseOptionalWalletTransferUserId(c.Query("user_id"))
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("Invalid user ID"))
		return
	}
	page := common.GetPageQuery(c)
	keyword := strings.TrimSpace(c.Query("keyword"))
	dateKey := c.Query("date")
	records, total, totalFee, err := model.SearchWalletTransfers(userId, keyword, dateKey, page.GetStartIdx(), page.GetPageSize())
	if err != nil {
		common.ApiError(c, err)
		return
	}
	page.SetTotal(int(total))
	page.SetItems(records)
	common.ApiSuccess(c, gin.H{"page": page, "total_fee": totalFee})
}

func parseOptionalWalletTransferUserId(value string) (int, error) {
	if value == "" {
		return 0, nil
	}
	userId, err := strconv.Atoi(value)
	if err != nil || userId < 1 {
		return 0, errors.New("invalid user id")
	}
	return userId, nil
}

func writeWalletTransferError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, model.ErrWalletTransferInvalid), errors.Is(err, model.ErrWalletTransferBelowMinimum), errors.Is(err, model.ErrWalletTransferQuoteInvalid), errors.Is(err, model.ErrWalletPasswordFlowInvalid), errors.Is(err, model.ErrWalletPasswordCodeInvalid), errors.Is(err, model.ErrWalletPasswordSameAsLogin), errors.Is(err, model.ErrWalletPasswordUnchanged), errors.Is(err, common.ErrAccountPasswordLength), errors.Is(err, common.ErrPasswordTooCommon), errors.Is(err, common.ErrPasswordLegacyLimit):
		common.ApiErrorStatus(c, http.StatusBadRequest, err)
	case errors.Is(err, model.ErrWalletTransferOutcomeUnknown):
		// 5xx on purpose: the client keeps the review and checks with it again.
		common.ApiErrorStatus(c, http.StatusServiceUnavailable, err)
	case errors.Is(err, service.ErrWalletTransferPasswordEmailDelivery):
		common.ApiErrorStatus(c, http.StatusBadGateway, err)
	case errors.Is(err, model.ErrWalletTransferRecipientInvalid):
		common.ApiErrorStatus(c, http.StatusBadRequest, err)
	// 409, not 5xx: the refusal is final and nothing moved, so the client must
	// not treat it as an unknown outcome.
	case errors.Is(err, model.ErrWalletTransferInsufficient), errors.Is(err, model.ErrWalletTransferBalanceUnverified), errors.Is(err, model.ErrWalletTransferPreviousPending), errors.Is(err, model.ErrWalletQuotaLimitExceeded):
		common.ApiErrorStatus(c, http.StatusConflict, err)
	case errors.Is(err, model.ErrWalletTransferDailyLimit), errors.Is(err, model.ErrWalletTransferAttemptLimit):
		common.ApiErrorStatus(c, http.StatusTooManyRequests, err)
	case errors.Is(err, model.ErrWalletTransferPasswordInvalid), errors.Is(err, model.ErrWalletTransferLoginInvalid), errors.Is(err, model.ErrWalletTransferTwoFAInvalid), errors.Is(err, model.ErrWalletPasswordCurrentInvalid):
		// Not 401: the session is valid, and the web client answers 401 by
		// refreshing the session and replaying the request.
		common.ApiErrorStatus(c, http.StatusForbidden, err)
	case errors.Is(err, model.ErrWalletPasswordFlowLocked), errors.Is(err, model.ErrWalletTransferPasswordLocked), errors.Is(err, model.ErrWalletTransferTwoFALocked):
		common.ApiErrorStatus(c, http.StatusTooManyRequests, err)
	case errors.Is(err, model.ErrWalletTransferPasswordRequired), errors.Is(err, model.ErrWalletTransferLoginRequired), errors.Is(err, model.ErrEmailBindingRequired):
		common.ApiErrorStatus(c, http.StatusPreconditionRequired, err)
	case errors.Is(err, model.ErrWalletTransferTwoFAReplay), errors.Is(err, model.ErrWalletTransferPasswordState):
		common.ApiErrorStatus(c, http.StatusConflict, err)
	default:
		common.ApiError(c, err)
	}
}
