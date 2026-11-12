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

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/shopspring/decimal"
	"github.com/smartwalle/alipay/v3"
)

// AlipayF2F uses Alipay's RSA2 public-key mode.
type AlipayF2F struct {
	Client                     *alipay.Client
	AppID, SellerID, NotifyURL string
	Enabled                    bool
}

// AlipayF2FConfig is persisted as one protected option. PrivateKey and
// AlipayPublicKey are intentionally omitted from the admin GET response.
type AlipayF2FConfig struct {
	Enabled         bool   `json:"enabled"`
	Sandbox         bool   `json:"sandbox"`
	AppID           string `json:"app_id"`
	SellerID        string `json:"seller_id"`
	NotifyURL       string `json:"notify_url"`
	PrivateKey      string `json:"private_key"`
	AlipayPublicKey string `json:"alipay_public_key"`
}

var alipayF2FState struct {
	sync.Mutex
	fingerprint string
	client      *AlipayF2F
	err         error
}

func ValidateAlipayF2FConfig(config AlipayF2FConfig) (*AlipayF2F, error) {
	appID := strings.TrimSpace(config.AppID)
	sellerID := strings.TrimSpace(config.SellerID)
	notifyURL := strings.TrimSpace(config.NotifyURL)
	u, err := url.Parse(notifyURL)
	if appID == "" || sellerID == "" || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "/api/alipay-f2f/notify" {
		return nil, errors.New("Alipay F2F requires app ID, seller ID and an HTTPS notification URL ending in /api/alipay-f2f/notify")
	}
	privateKey := []byte(config.PrivateKey)
	publicKey := []byte(config.AlipayPublicKey)
	if len(privateKey) == 0 || len(publicKey) == 0 {
		return nil, errors.New("Alipay F2F private key and Alipay public key are required")
	}
	options := []alipay.OptionFunc{alipay.WithHTTPClient(&http.Client{Timeout: 15 * time.Second})}
	if config.Sandbox {
		options = append(options, alipay.WithNewSandboxGateway())
	}
	client, err := alipay.New(appID, string(privateKey), !config.Sandbox, options...)
	if err != nil {
		return nil, errors.New("invalid Alipay F2F private key")
	}
	if err := client.LoadAliPayPublicKey(string(publicKey)); err != nil {
		return nil, errors.New("invalid Alipay public key")
	}
	return &AlipayF2F{Client: client, AppID: appID, SellerID: sellerID, NotifyURL: notifyURL, Enabled: config.Enabled}, nil
}

func envAlipayF2FConfig() (AlipayF2FConfig, error) {
	config := AlipayF2FConfig{
		Enabled: os.Getenv("ALIPAY_F2F_ENABLED") == "true", Sandbox: os.Getenv("ALIPAY_F2F_SANDBOX") == "true",
		AppID: os.Getenv("ALIPAY_F2F_APP_ID"), SellerID: os.Getenv("ALIPAY_F2F_SELLER_ID"), NotifyURL: os.Getenv("ALIPAY_F2F_NOTIFY_URL"),
	}
	readFile := func(name string) (string, error) {
		contents, err := os.ReadFile(name)
		if err != nil {
			return "", errors.New("cannot read Alipay F2F key file")
		}
		return string(contents), nil
	}
	var err error
	config.PrivateKey, err = readFile(os.Getenv("ALIPAY_F2F_PRIVATE_KEY_FILE"))
	if err != nil {
		return config, err
	}
	config.AlipayPublicKey, err = readFile(os.Getenv("ALIPAY_F2F_PUBLIC_KEY_FILE"))
	return config, err
}

// LoadAlipayF2FConfig reads the single persisted snapshot. An empty database
// option selects the legacy environment/file configuration.
func LoadAlipayF2FConfig() (config AlipayF2FConfig, raw string, err error) {
	common.OptionMapRWMutex.RLock()
	encoded := common.OptionMap[model.AlipayF2FOptionKey]
	common.OptionMapRWMutex.RUnlock()
	if strings.TrimSpace(encoded) == "" {
		config, err := envAlipayF2FConfig()
		return config, "", err
	}
	if err := common.UnmarshalJsonStr(encoded, &config); err != nil {
		return config, encoded, errors.New("invalid persisted Alipay F2F configuration")
	}
	return config, encoded, nil
}

// GetAlipayF2F returns the current effective configuration. It rebuilds the
// SDK client only when the persisted option or environment fallback changes.
func GetAlipayF2F() (*AlipayF2F, error) {
	config, _, err := LoadAlipayF2FConfig()
	if err != nil {
		return nil, err
	}
	encoded, err := common.Marshal(config)
	if err != nil {
		return nil, err
	}
	alipayF2FState.Lock()
	defer alipayF2FState.Unlock()
	if string(encoded) == alipayF2FState.fingerprint {
		return alipayF2FState.client, alipayF2FState.err
	}
	alipayF2FState.fingerprint = string(encoded)
	alipayF2FState.client, alipayF2FState.err = ValidateAlipayF2FConfig(config)
	return alipayF2FState.client, alipayF2FState.err
}

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
	// This integration only supports public-key mode. An untrusted certificate
	// serial would make the SDK fetch a certificate while holding its client
	// mutex, allowing unsigned callbacks to stall all payment requests.
	if values.Get("alipay_cert_sn") != "" {
		return errors.New("Alipay certificate mode is not supported")
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
