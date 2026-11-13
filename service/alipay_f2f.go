package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/smartwalle/alipay/v3"
)

// AlipayF2F uses Alipay's RSA2 public-key mode.
type AlipayF2F struct {
	Client                     *alipay.Client
	AppID, SellerID, NotifyURL string
	Enabled                    bool
	NotifyAlipayApp            bool
}

// Where the application private key lives once settings are saved. The legacy
// value marks configurations saved before key storage existed; it is readable
// but never written again.
const (
	AlipayF2FKeyStorageFile      = "file"
	AlipayF2FKeyStorageEncrypted = "encrypted"
	AlipayF2FKeyStorageLegacy    = "database"
)

// ErrAlipayF2FPrivateKeyUnavailable means the settings exist but this node
// cannot read the key: the key file is absent or CRYPTO_SECRET differs.
var ErrAlipayF2FPrivateKeyUnavailable = errors.New("Alipay F2F private key is unavailable on this node")

// AlipayF2FConfig is the resolved configuration held in memory. PrivateKey and
// AlipayPublicKey are intentionally omitted from the admin GET response.
type AlipayF2FConfig struct {
	Enabled         bool   `json:"enabled"`
	Sandbox         bool   `json:"sandbox"`
	NotifyAlipayApp bool   `json:"notify_alipay_app"`
	AppID           string `json:"app_id"`
	SellerID        string `json:"seller_id"`
	NotifyURL       string `json:"notify_url"`
	PrivateKey      string `json:"private_key"`
	AlipayPublicKey string `json:"alipay_public_key"`
	KeyStorage      string `json:"key_storage"`
}

// alipayF2FStoredConfig is the database form. New saves leave PrivateKey empty
// and carry either the key file name or the ciphertext.
type alipayF2FStoredConfig struct {
	AlipayF2FConfig
	PrivateKeyFile      string `json:"private_key_file,omitempty"`
	PrivateKeyEncrypted string `json:"private_key_encrypted,omitempty"`
}

const alipayF2FKeyCipherContext = "alipay-f2f-private-key-v1"

var alipayF2FKeyFileName = regexp.MustCompile(`^private-[0-9a-f]{32}\.pem$`)

// The default is relative to the working directory, which is the persistent
// /data volume in the official container image.
func alipayF2FKeyFilePath(name string) (string, error) {
	if !alipayF2FKeyFileName.MatchString(name) {
		return "", errors.New("invalid Alipay F2F key file name")
	}
	dir := os.Getenv("ALIPAY_F2F_KEY_DIR")
	if dir == "" {
		dir = "alipay-f2f"
	}
	return filepath.Join(dir, name), nil
}

// AlipayF2FEncryptionAvailable requires an explicit CRYPTO_SECRET. The session
// secret fallback and the per-start random default would make ciphertext
// unreadable after a session rotation or a restart.
func AlipayF2FEncryptionAvailable() bool {
	return os.Getenv("CRYPTO_SECRET") != ""
}

func alipayF2FKeyCipher() (cipher.AEAD, error) {
	if !AlipayF2FEncryptionAvailable() {
		return nil, common.NewMessage("Set the CRYPTO_SECRET environment variable to enable encrypted storage.")
	}
	key, err := hkdf.Key(sha256.New, []byte(common.CryptoSecret), nil, alipayF2FKeyCipherContext, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// SealAlipayF2FConfig builds the database value for a validated configuration
// without the plaintext private key. finish must be called with the outcome of
// the database write: it removes a newly created file on a failed write.
// Replaced files are retained: another node may still hold the previous option
// snapshot while it finishes a callback or refreshes its configuration cache.
func SealAlipayF2FConfig(config AlipayF2FConfig, previousRaw string) (encoded string, finish func(saved bool), err error) {
	var previous alipayF2FStoredConfig
	if previousRaw != "" {
		_ = common.UnmarshalJsonStr(previousRaw, &previous)
	}
	stored := alipayF2FStoredConfig{AlipayF2FConfig: config}
	stored.PrivateKey = ""
	createdPath := ""
	switch config.KeyStorage {
	case AlipayF2FKeyStorageFile:
		stored.PrivateKeyFile = previous.PrivateKeyFile
		existing := ""
		if existingPath, pathErr := alipayF2FKeyFilePath(previous.PrivateKeyFile); pathErr == nil {
			contents, _ := os.ReadFile(existingPath)
			existing = string(contents)
		}
		if existing != config.PrivateKey {
			created := false
			stored.PrivateKeyFile = "private-" + strings.ReplaceAll(uuid.NewString(), "-", "") + ".pem"
			createdPath, err = alipayF2FKeyFilePath(stored.PrivateKeyFile)
			if err == nil {
				err = os.MkdirAll(filepath.Dir(createdPath), 0700)
			}
			if err == nil {
				var file *os.File
				file, err = os.OpenFile(createdPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err == nil {
					created = true
					_, err = file.WriteString(config.PrivateKey)
					closeErr := file.Close()
					if err == nil {
						err = closeErr
					}
				}
			}
			if err != nil {
				if created {
					_ = os.Remove(createdPath)
				}
				return "", nil, common.NewMessage("Unable to write the Alipay private key file. Check the data directory permissions.")
			}
		}
	case AlipayF2FKeyStorageEncrypted:
		aead, err := alipayF2FKeyCipher()
		if err != nil {
			return "", nil, err
		}
		nonce := make([]byte, aead.NonceSize())
		if _, err := rand.Read(nonce); err != nil {
			return "", nil, err
		}
		sealed := aead.Seal(nonce, nonce, []byte(config.PrivateKey), []byte(alipayF2FKeyCipherContext))
		stored.PrivateKeyEncrypted = "v1." + base64.StdEncoding.EncodeToString(sealed)
	default:
		return "", nil, errors.New("invalid Alipay F2F key storage")
	}
	value, err := common.Marshal(stored)
	if err != nil {
		if createdPath != "" {
			_ = os.Remove(createdPath)
		}
		return "", nil, err
	}
	finish = func(saved bool) {
		if !saved {
			if createdPath != "" {
				_ = os.Remove(createdPath)
			}
			return
		}
	}
	return string(value), finish, nil
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
	return &AlipayF2F{Client: client, AppID: appID, SellerID: sellerID, NotifyURL: notifyURL, Enabled: config.Enabled, NotifyAlipayApp: config.NotifyAlipayApp}, nil
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

// LoadAlipayF2FConfig reads the single persisted snapshot and resolves the
// private key from its storage. An empty database option selects the legacy
// environment/file configuration. With ErrAlipayF2FPrivateKeyUnavailable the
// returned configuration still carries every non-secret field.
func LoadAlipayF2FConfig() (config AlipayF2FConfig, raw string, err error) {
	common.OptionMapRWMutex.RLock()
	encoded := common.OptionMap[model.AlipayF2FOptionKey]
	common.OptionMapRWMutex.RUnlock()
	if strings.TrimSpace(encoded) == "" {
		config, err := envAlipayF2FConfig()
		return config, "", err
	}
	var stored alipayF2FStoredConfig
	if err := common.UnmarshalJsonStr(encoded, &stored); err != nil {
		return config, encoded, errors.New("invalid persisted Alipay F2F configuration")
	}
	config = stored.AlipayF2FConfig
	switch {
	case stored.PrivateKeyFile != "":
		config.KeyStorage = AlipayF2FKeyStorageFile
		path, err := alipayF2FKeyFilePath(stored.PrivateKeyFile)
		if err != nil {
			return config, encoded, ErrAlipayF2FPrivateKeyUnavailable
		}
		contents, err := os.ReadFile(path)
		if err != nil || len(contents) == 0 {
			return config, encoded, ErrAlipayF2FPrivateKeyUnavailable
		}
		config.PrivateKey = string(contents)
	case stored.PrivateKeyEncrypted != "":
		config.KeyStorage = AlipayF2FKeyStorageEncrypted
		aead, err := alipayF2FKeyCipher()
		if err != nil {
			return config, encoded, ErrAlipayF2FPrivateKeyUnavailable
		}
		payload, ok := strings.CutPrefix(stored.PrivateKeyEncrypted, "v1.")
		sealed, err := base64.StdEncoding.DecodeString(payload)
		if !ok || err != nil || len(sealed) <= aead.NonceSize() {
			return config, encoded, ErrAlipayF2FPrivateKeyUnavailable
		}
		plaintext, err := aead.Open(nil, sealed[:aead.NonceSize()], sealed[aead.NonceSize():], []byte(alipayF2FKeyCipherContext))
		if err != nil {
			return config, encoded, ErrAlipayF2FPrivateKeyUnavailable
		}
		config.PrivateKey = string(plaintext)
	default:
		config.KeyStorage = AlipayF2FKeyStorageLegacy
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

// The upstream SDK omits qr_code_timeout_express; embedding preserves its
// signed Param contract while adding this official precreate parameter.
type alipayF2FPrecreate struct {
	alipay.TradePreCreate
	QRCodeTimeoutExpress string `json:"qr_code_timeout_express"`
}

func (a *AlipayF2F) Precreate(ctx context.Context, tradeNo, amount string, expiresAt time.Time) (string, error) {
	if _, err := AlipayAmount(amount); err != nil {
		return "", err
	}
	// Alipay requires this opt-in for app receipt notifications / voice alerts.
	// Omit it when disabled; this does not control our payment callback.
	var businessParams []byte
	if a.NotifyAlipayApp {
		businessParams = []byte(`{"notify_alipay_app":"true"}`)
	}
	rsp := new(alipay.TradePreCreateRsp)
	err := a.Client.Request(ctx, alipayF2FPrecreate{TradePreCreate: alipay.TradePreCreate{Trade: alipay.Trade{
		BusinessParams: businessParams,
		OutTradeNo:     tradeNo, TotalAmount: amount, Subject: "New Utopia account top-up",
		ProductCode: "FACE_TO_FACE_PAYMENT", NotifyURL: a.NotifyURL, SellerId: a.SellerID, TimeoutExpress: "5m",
		TimeExpire: expiresAt.In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05"),
	}}, QRCodeTimeoutExpress: "5m"}, rsp)
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

// Close only closes unpaid trades. Never use trade.cancel here: that API may
// refund a paid trade. A failed close must be reconciled with a signed query.
func (a *AlipayF2F) Close(ctx context.Context, tradeNo string) (bool, error) {
	rsp, err := a.Client.TradeClose(ctx, alipay.TradeClose{OutTradeNo: tradeNo})
	if err != nil {
		return false, err
	}
	if rsp == nil {
		return false, errors.New("empty Alipay close response")
	}
	if rsp.IsFailure() {
		return false, nil
	}
	if rsp.OutTradeNo != tradeNo || rsp.TradeNo == "" {
		return false, errors.New("Alipay close order mismatch")
	}
	return true, nil
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
		// An unscanned QR code has no trade yet; callers decide when that is final.
		if rsp.SubCode == "ACQ.TRADE_NOT_EXIST" {
			return "TRADE_NOT_EXIST", "", nil
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
