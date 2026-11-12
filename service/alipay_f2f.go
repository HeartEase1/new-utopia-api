package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"github.com/smartwalle/alipay/v3"
)

// AlipayF2F uses public-key mode. Secrets are read from mounted files, never
// returned by the options API. Restart after changing credentials.
type AlipayF2F struct {
	Client                     *alipay.Client
	AppID, SellerID, NotifyURL string
	Enabled                    bool
}

var loadAlipayF2F = sync.OnceValues(func() (*AlipayF2F, error) {
	appID := strings.TrimSpace(os.Getenv("ALIPAY_F2F_APP_ID"))
	sellerID := strings.TrimSpace(os.Getenv("ALIPAY_F2F_SELLER_ID"))
	notifyURL := strings.TrimSpace(os.Getenv("ALIPAY_F2F_NOTIFY_URL"))
	u, err := url.Parse(notifyURL)
	if appID == "" || sellerID == "" || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/api/alipay-f2f/notify" {
		return nil, errors.New("Alipay F2F requires app ID, seller ID and an HTTPS notification URL ending in /api/alipay-f2f/notify")
	}
	privateKey, err := os.ReadFile(os.Getenv("ALIPAY_F2F_PRIVATE_KEY_FILE"))
	if err != nil {
		return nil, errors.New("cannot read Alipay F2F private key file")
	}
	publicKey, err := os.ReadFile(os.Getenv("ALIPAY_F2F_PUBLIC_KEY_FILE"))
	if err != nil {
		return nil, errors.New("cannot read Alipay F2F public key file")
	}
	client, err := alipay.New(appID, string(privateKey), os.Getenv("ALIPAY_F2F_SANDBOX") != "true",
		alipay.WithHTTPClient(&http.Client{Timeout: 15 * time.Second}), alipay.WithNewSandboxGateway())
	if err != nil {
		return nil, errors.New("invalid Alipay F2F private key")
	}
	if err := client.LoadAliPayPublicKey(string(publicKey)); err != nil {
		return nil, errors.New("invalid Alipay F2F public key")
	}
	return &AlipayF2F{Client: client, AppID: appID, SellerID: sellerID, NotifyURL: notifyURL, Enabled: os.Getenv("ALIPAY_F2F_ENABLED") == "true"}, nil
})

func GetAlipayF2F() (*AlipayF2F, error) { return loadAlipayF2F() }

// AlipayAmount parses protocol amounts without float comparison or rounding a
// mismatched payment into a match. total_amount includes discounts paid by Alipay.
func AlipayAmount(raw string) (decimal.Decimal, error) {
	if len(raw) == 0 || len(raw) > 12 {
		return decimal.Zero, errors.New("invalid Alipay amount")
	}
	for _, c := range raw {
		if (c < '0' || c > '9') && c != '.' {
			return decimal.Zero, errors.New("invalid Alipay amount")
		}
	}
	amount, err := decimal.NewFromString(raw)
	if err != nil || !amount.IsPositive() || amount.Exponent() < -2 || amount.GreaterThan(decimal.NewFromInt(100000000)) {
		return decimal.Zero, errors.New("invalid Alipay amount")
	}
	return amount, nil
}

func (a *AlipayF2F) Precreate(ctx context.Context, tradeNo, amount string) (string, error) {
	if _, err := AlipayAmount(amount); err != nil {
		return "", err
	}
	rsp, err := a.Client.TradePreCreate(ctx, alipay.TradePreCreate{Trade: alipay.Trade{
		OutTradeNo: tradeNo, TotalAmount: amount, Subject: "New Utopia account top-up",
		ProductCode: "FACE_TO_FACE_PAYMENT", NotifyURL: a.NotifyURL, SellerId: a.SellerID, TimeoutExpress: "15m",
	}})
	if err != nil {
		return "", err
	}
	if rsp == nil {
		return "", errors.New("empty Alipay response")
	}
	if rsp.IsFailure() {
		return "", fmt.Errorf("Alipay precreate failed: %s", rsp.SubCode)
	}
	if rsp.OutTradeNo != tradeNo || rsp.QRCode == "" {
		return "", errors.New("invalid Alipay precreate response")
	}
	return rsp.QRCode, nil
}

// VerifyNotification accepts POST form fields only. Checking merchant identity
// is mandatory: a valid Alipay signature alone does not identify our merchant.
func (a *AlipayF2F) VerifyNotification(ctx context.Context, values url.Values) error {
	for _, entries := range values {
		if len(entries) != 1 {
			return errors.New("duplicate Alipay notification field")
		}
	}
	if values.Get("sign_type") != "RSA2" || values.Get("app_id") != a.AppID || values.Get("seller_id") != a.SellerID || values.Get("trade_no") == "" || values.Get("out_trade_no") == "" {
		return errors.New("Alipay notification identity mismatch")
	}
	if _, err := AlipayAmount(values.Get("total_amount")); err != nil {
		return err
	}
	return a.Client.VerifySign(ctx, values)
}

// Query uses an authenticated, signed API response. Unlike notifications, this
// response has no app_id/seller_id; the app credentials scope the query.
func (a *AlipayF2F) Query(ctx context.Context, tradeNo string) (status, amount string, err error) {
	rsp, err := a.Client.TradeQuery(ctx, alipay.TradeQuery{OutTradeNo: tradeNo})
	if err != nil {
		return "", "", err
	}
	if rsp == nil {
		return "", "", errors.New("empty Alipay query response")
	}
	if rsp.IsFailure() {
		if rsp.SubCode == "ACQ.TRADE_NOT_EXIST" {
			return "WAIT_BUYER_PAY", "", nil
		}
		return "", "", fmt.Errorf("Alipay query failed: %s", rsp.SubCode)
	}
	if rsp.OutTradeNo != tradeNo || rsp.TradeNo == "" {
		return "", "", errors.New("Alipay query order mismatch")
	}
	if _, err := AlipayAmount(rsp.TotalAmount); err != nil {
		return "", "", err
	}
	return string(rsp.TradeStatus), rsp.TotalAmount, nil
}
