package controller

import (
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
	if rejectInvalidTopUpQuota(c, id, req.Amount) {
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
	order := model.TopUp{UserId: id, Amount: amount, Money: paidAmount.InexactFloat64(),
		TradeNo: "UT" + strings.ReplaceAll(uuid.NewString(), "-", ""), PaymentMethod: model.PaymentProviderAlipayF2F,
		PaymentProvider: model.PaymentProviderAlipayF2F, Status: common.TopUpStatusPending, CreateTime: time.Now().Unix()}
	if err := order.Insert(); err != nil {
		paymentError(c, common.NewMessage("Failed to create order"))
		return
	}
	qrCode, err := client.Precreate(c.Request.Context(), order.TradeNo, amountText)
	if err != nil {
		// A timeout is ambiguous: keep pending so a delayed payment can settle.
		logger.LogError(c.Request.Context(), common.LogText("Alipay F2F precreate failed trade_no=%s", order.TradeNo))
		paymentError(c, common.NewMessage("Failed to start payment"))
		return
	}
	common.ApiSuccess(c, gin.H{"trade_no": order.TradeNo, "qr_code": qrCode, "amount": amountText, "expires_at": order.CreateTime + 15*60})
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
	status := values.Get("trade_status")
	if status != "TRADE_SUCCESS" && status != "TRADE_FINISHED" {
		c.String(http.StatusOK, "success")
		return
	}
	amount, err := service.AlipayAmount(values.Get("total_amount"))
	if err != nil {
		c.String(http.StatusBadRequest, "failure")
		return
	}
	tradeNo := values.Get("out_trade_no")
	LockOrder(tradeNo)
	defer UnlockOrder(tradeNo)
	if err := model.RechargeAlipayF2F(tradeNo, amount, c.ClientIP()); err != nil {
		logger.LogWarn(c.Request.Context(), common.LogText("Alipay F2F settlement failed trade_no=%s", tradeNo))
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
	if status == "TRADE_SUCCESS" || status == "TRADE_FINISHED" {
		amount, err := service.AlipayAmount(total)
		if err != nil {
			common.ApiErrorT(c, "Failed to query payment status")
			return
		}
		LockOrder(order.TradeNo)
		defer UnlockOrder(order.TradeNo)
		if err := model.RechargeAlipayF2F(order.TradeNo, amount, c.ClientIP()); err != nil {
			common.ApiErrorT(c, "Failed to complete payment")
			return
		}
		order.Status = common.TopUpStatusSuccess
	} else if status == "TRADE_CLOSED" {
		order.Status = common.TopUpStatusExpired
	}
	// Local time alone never expires a DB order: delayed paid notifications must
	// still be accepted. Closing the dialog is also not a cancellation.
	common.ApiSuccess(c, gin.H{"status": order.Status})
}
