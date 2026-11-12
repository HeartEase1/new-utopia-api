package controller

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var getAlipayF2F = service.GetAlipayF2F

// A QR code that was never scanned has no Alipay trade, so a missing trade is
// final only once the code can no longer be scanned and paid. Three hours
// exceeds Alipay's QR code lifetime plus the 15-minute payment window.
const alipayF2FOrderLifetime = 3 * time.Hour

const alipayF2FPaymentWindow = 5 * time.Minute

func alipayF2FMissingTradeExpired(order *model.TopUp) bool {
	if order.ExpiresAt > 0 {
		return time.Now().Unix() >= order.ExpiresAt
	}
	return time.Since(time.Unix(order.CreateTime, 0)) >= alipayF2FOrderLifetime
}

// applyAlipayF2FTradeStatus records a trade state that Alipay signed, from a
// notification or a query: a paid trade credits the wallet and a closed trade
// expires the order. It returns the resulting order status, or "" when the
// state changes nothing.
func applyAlipayF2FTradeStatus(ctx context.Context, tradeNo, status, total, callerIP string) (string, error) {
	switch status {
	case "TRADE_SUCCESS", "TRADE_FINISHED":
		amount, err := service.AlipayAmount(total)
		if err == nil {
			LockOrder(tradeNo)
			defer UnlockOrder(tradeNo)
			err = model.RechargeAlipayF2F(tradeNo, amount, callerIP)
		}
		if err != nil {
			// The buyer has paid; the reason is what an administrator needs to act on.
			logger.LogError(ctx, common.LogText("Alipay F2F paid order was not credited trade_no=%s paid_amount=%s reason=%q", tradeNo, total, err.Error()))
			return "", err
		}
		return common.TopUpStatusSuccess, nil
	case "TRADE_CLOSED":
		err := model.UpdatePendingTopUpStatus(tradeNo, model.PaymentProviderAlipayF2F, common.TopUpStatusExpired)
		if errors.Is(err, model.ErrTopUpNotFound) || errors.Is(err, model.ErrPaymentMethodMismatch) || errors.Is(err, model.ErrTopUpStatusInvalid) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		return common.TopUpStatusExpired, nil
	}
	return "", nil
}

func isAlipayF2FTopUpEnabled() bool {
	client, err := getAlipayF2F()
	return err == nil && client.Enabled && isPaymentComplianceConfirmed()
}

func RequestAlipayF2F(c *gin.Context) {
	client, err := getAlipayF2F()
	if err != nil || !client.Enabled || !isPaymentComplianceConfirmed() {
		paymentError(c, common.NewMessage("Payment information has not been configured by administrator"))
		return
	}
	var req AmountRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Amount < getMinTopup() {
		paymentError(c, common.NewMessage("Invalid top-up amount"))
		return
	}
	id := c.GetInt("id")
	quotedQuota := decimal.NewFromInt(req.Amount)
	if operation_setting.GetQuotaDisplayType() != operation_setting.QuotaDisplayTypeTokens {
		quotedQuota = quotedQuota.Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	}
	creditedQuota, err := validateCreditedQuota(quotedQuota)
	if err == nil {
		err = model.ValidateTopUpQuotaCapacity(id, creditedQuota)
	}
	if err != nil {
		paymentError(c, err)
		return
	}
	group, err := model.GetUserGroup(id, true)
	if err != nil {
		paymentError(c, common.NewMessage("Failed to get the user group"))
		return
	}
	money := getPayMoney(req.Amount, group)
	if math.IsNaN(money) || math.IsInf(money, 0) {
		paymentError(c, common.NewMessage("Invalid top-up amount"))
		return
	}
	// Freeze the actual two-decimal amount sent to Alipay, including discounts.
	amountText := fmt.Sprintf("%.2f", money)
	paidAmount, err := service.AlipayAmount(amountText)
	if err != nil {
		paymentError(c, common.NewMessage("Invalid top-up amount"))
		return
	}
	amount := req.Amount
	if operation_setting.GetQuotaDisplayType() == operation_setting.QuotaDisplayTypeTokens {
		amount = decimal.NewFromInt(amount).Div(decimal.NewFromFloat(common.QuotaPerUnit)).IntPart()
	}
	order := model.TopUp{UserId: id, Amount: amount, QuotaSnapshot: int64(creditedQuota), Money: paidAmount.InexactFloat64(),
		TradeNo: "UT" + strings.ReplaceAll(uuid.NewString(), "-", ""), PaymentMethod: model.PaymentProviderAlipayF2F,
		PaymentProvider: model.PaymentProviderAlipayF2F, Status: common.TopUpStatusPending, CreateTime: time.Now().Unix()}
	order.ExpiresAt = order.CreateTime + int64(alipayF2FPaymentWindow/time.Second)
	if err := order.Insert(); err != nil {
		paymentError(c, common.NewMessage("Failed to create order"))
		return
	}
	qrCode, err := client.Precreate(c.Request.Context(), order.TradeNo, amountText, time.Unix(order.ExpiresAt, 0))
	if err != nil {
		// A timeout is ambiguous: keep pending so a delayed payment can settle.
		logger.LogError(c.Request.Context(), common.LogText("Alipay F2F precreate failed trade_no=%s", order.TradeNo))
		paymentError(c, common.NewMessage("Failed to start payment"))
		return
	}
	common.ApiSuccess(c, gin.H{"trade_no": order.TradeNo, "qr_code": qrCode, "amount": amountText, "expires_at": order.ExpiresAt})
}

func AlipayF2FNotify(c *gin.Context) {
	client, err := getAlipayF2F()
	if err != nil {
		c.String(http.StatusServiceUnavailable, "failure")
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64*1024)
	if err := c.Request.ParseForm(); err != nil {
		c.String(http.StatusBadRequest, "failure")
		return
	}
	values := c.Request.PostForm
	if err := client.VerifyNotification(c.Request.Context(), values); err != nil {
		c.String(http.StatusBadRequest, "failure")
		return
	}
	// A failure answer makes Alipay deliver the notification again.
	if _, err := applyAlipayF2FTradeStatus(c.Request.Context(), values.Get("out_trade_no"), values.Get("trade_status"), values.Get("total_amount"), c.ClientIP()); err != nil {
		c.String(http.StatusInternalServerError, "failure")
		return
	}
	// Disabling new orders must not strand payments already in flight.
	c.String(http.StatusOK, "success")
}

func QueryAlipayF2FOrder(c *gin.Context) {
	var req struct {
		TradeNo string `json:"trade_no" binding:"required,max=64"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var order model.TopUp
	if err := model.DB.Where("trade_no = ? AND user_id = ? AND payment_provider = ?", req.TradeNo, c.GetInt("id"), model.PaymentProviderAlipayF2F).First(&order).Error; err != nil {
		common.ApiErrorT(c, "Top-up order does not exist")
		return
	}
	if order.Status == common.TopUpStatusSuccess {
		common.ApiSuccess(c, gin.H{"status": order.Status})
		return
	}
	client, err := getAlipayF2F()
	if err != nil {
		common.ApiErrorT(c, "Payment information has not been configured by administrator")
		return
	}
	status, total, err := client.Query(c.Request.Context(), order.TradeNo)
	if err != nil {
		common.ApiErrorT(c, "Failed to query payment status")
		return
	}
	if status == "TRADE_NOT_EXIST" && alipayF2FMissingTradeExpired(&order) {
		status = "TRADE_CLOSED"
	}
	applied, err := applyAlipayF2FTradeStatus(c.Request.Context(), order.TradeNo, status, total, c.ClientIP())
	if err != nil {
		common.ApiErrorT(c, "Failed to complete payment")
		return
	}
	if applied != "" {
		order.Status = applied
	}
	common.ApiSuccess(c, gin.H{"status": order.Status})
}

// closeAlipayF2FOrder reconciles a cancellation/expiry against Alipay. Local
// expiration is never used to discard an authenticated payment notification.
func closeAlipayF2FOrder(ctx context.Context, client *service.AlipayF2F, order *model.TopUp, callerIP string) (string, error) {
	closed, closeErr := client.Close(ctx, order.TradeNo)
	status, amount := "TRADE_CLOSED", ""
	if !closed {
		var err error
		status, amount, err = client.Query(ctx, order.TradeNo)
		if err != nil {
			return "", err
		}
		if status == "TRADE_NOT_EXIST" && alipayF2FMissingTradeExpired(order) {
			status = "TRADE_CLOSED"
		} else if status == "WAIT_BUYER_PAY" {
			if closeErr != nil {
				return "", closeErr
			}
			return "", errors.New("Alipay has not closed the unpaid trade")
		}
	}
	if _, err := applyAlipayF2FTradeStatus(ctx, order.TradeNo, status, amount, callerIP); err != nil {
		return "", err
	}
	// Re-read after a concurrent callback: success must win over cancellation.
	var current model.TopUp
	if err := model.DB.WithContext(ctx).Where("id = ?", order.Id).First(&current).Error; err != nil {
		return "", err
	}
	return current.Status, nil
}

func CancelAlipayF2FOrder(c *gin.Context) {
	var req struct {
		TradeNo string `json:"trade_no" binding:"required,max=64"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return
	}
	var order model.TopUp
	if err := model.DB.Where("trade_no = ? AND user_id = ? AND payment_provider = ?", req.TradeNo, c.GetInt("id"), model.PaymentProviderAlipayF2F).First(&order).Error; err != nil {
		common.ApiErrorT(c, "Top-up order does not exist")
		return
	}
	if order.Status == common.TopUpStatusSuccess || order.Status == common.TopUpStatusExpired {
		common.ApiSuccess(c, gin.H{"status": order.Status})
		return
	}
	client, err := getAlipayF2F()
	if err != nil {
		common.ApiErrorT(c, "Payment information has not been configured by administrator")
		return
	}
	status, err := closeAlipayF2FOrder(c.Request.Context(), client, &order, c.ClientIP())
	if err != nil {
		common.ApiErrorT(c, "Unable to cancel payment. Check the payment status and try again.")
		return
	}
	// Unscanned QR codes have no Alipay trade to close. Keep the order pending
	// for expiry reconciliation and tell the UI that the QR may remain valid.
	common.ApiSuccess(c, gin.H{"status": status})
}
