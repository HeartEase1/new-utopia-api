package controller

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/shopspring/decimal"
	"github.com/smartwalle/alipay/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// ALIPAY_TEST_DSN must name a disposable test database. No production config is
// read. CI exercises the same flow against all three supported database engines.
func TestAlipayF2F(t *testing.T) {
	dialect := os.Getenv("ALIPAY_TEST_DIALECT")
	var driver gorm.Dialector
	dbType := common.DatabaseTypeSQLite
	switch dialect {
	case "mysql":
		driver = mysql.Open(os.Getenv("ALIPAY_TEST_DSN"))
		dbType = common.DatabaseTypeMySQL
	case "postgres":
		driver = postgres.Open(os.Getenv("ALIPAY_TEST_DSN"))
		dbType = common.DatabaseTypePostgreSQL
	default:
		driver = sqlite.Open(filepath.Join(t.TempDir(), "payment.db") + "?_pragma=busy_timeout(30000)&_pragma=journal_mode(WAL)&_txlock=immediate")
	}
	db, err := gorm.Open(driver, &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = sqlDB.Close() })
	oldDB, oldLog, oldMainType, oldLogType := model.DB, model.LOG_DB, common.MainDatabaseType(), common.LogDatabaseType()
	oldRedis, oldBatch := common.RedisEnabled, common.BatchUpdateEnabled
	model.DB, model.LOG_DB = db, db
	common.SetDatabaseTypes(dbType, dbType)
	common.RedisEnabled, common.BatchUpdateEnabled = false, false
	t.Setenv("LOG_SQL_DSN", "")
	wasMaster := common.IsMasterNode
	common.IsMasterNode = false
	require.NoError(t, model.InitLogDB()) // initializes dialect-specific quoted columns
	common.IsMasterNode = wasMaster
	t.Cleanup(func() {
		model.DB, model.LOG_DB = oldDB, oldLog
		common.SetDatabaseTypes(oldMainType, oldLogType)
		common.RedisEnabled, common.BatchUpdateEnabled = oldRedis, oldBatch
	})
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
	// Re-run migration while an existing order is present; this feature adds no schema.
	user := model.User{Username: "utopia_alipay_test", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, Amount: 2, Money: 10.01, TradeNo: "UTexisting", PaymentMethod: model.PaymentProviderAlipayF2F, PaymentProvider: model.PaymentProviderAlipayF2F, Status: common.TopUpStatusPending}
	require.NoError(t, order.Insert())
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.TopUp{}, &model.Log{}))
	var version string
	switch dialect {
	case "mysql", "postgres":
		require.NoError(t, db.Raw("SELECT version()").Scan(&version).Error)
	default:
		require.NoError(t, db.Raw("SELECT sqlite_version()").Scan(&version).Error)
	}
	t.Logf("database=%s version=%s", dbType, version)
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	private := string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	publicBytes, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	public := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicBytes}))

	// The only mocked boundary is Alipay's network endpoint, including real RSA2
	// response signing and the SDK's response verification.
	queryAmount, queryStatus, queryOrder := "10.01", "TRADE_SUCCESS", ""
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		var biz struct {
			OutTradeNo  string `json:"out_trade_no"`
			TotalAmount string `json:"total_amount"`
			SellerID    string `json:"seller_id"`
		}
		require.NoError(t, common.UnmarshalJsonStr(r.Form.Get("biz_content"), &biz))
		responseName := "alipay_trade_query_response"
		tradeNo := biz.OutTradeNo
		if queryOrder != "" {
			tradeNo = queryOrder
		}
		body := map[string]any{"code": "10000", "trade_no": "ALI202601", "out_trade_no": tradeNo, "total_amount": queryAmount, "trade_status": queryStatus}
		if r.Form.Get("method") == "alipay.trade.precreate" {
			responseName = "alipay_trade_precreate_response"
			var saved model.TopUp
			require.NoError(t, db.Where("trade_no = ?", biz.OutTradeNo).First(&saved).Error)
			assert.Equal(t, "pending", saved.Status)
			assert.Equal(t, "10.01", biz.TotalAmount)
			assert.Equal(t, "seller-test", biz.SellerID)
			body = map[string]any{"code": "10000", "out_trade_no": biz.OutTradeNo, "qr_code": "https://qr.alipay.com/test-order"}
		}
		payload, err := common.Marshal(body)
		require.NoError(t, err)
		digest := sha256.Sum256(payload)
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		require.NoError(t, err)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"%s":%s,"sign":"%s"}`, responseName, payload, base64.StdEncoding.EncodeToString(signature))
	}))
	t.Cleanup(server.Close)
	sdk, err := alipay.New("app-test", private, true, alipay.WithProductionGateway(server.URL), alipay.WithHTTPClient(server.Client()))
	require.NoError(t, err)
	require.NoError(t, sdk.LoadAliPayPublicKey(public))
	client := &service.AlipayF2F{Client: sdk, AppID: "app-test", SellerID: "seller-test", NotifyURL: "https://test.example/api/alipay-f2f/notify", Enabled: true}
	oldClient := getAlipayF2F
	getAlipayF2F = func() (*service.AlipayF2F, error) { return client, nil }
	t.Cleanup(func() { getAlipayF2F = oldClient })
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/notify", AlipayF2FNotify)
	router.POST("/query", func(c *gin.Context) { c.Set("id", user.Id); QueryAlipayF2FOrder(c) })
	router.POST("/pay", func(c *gin.Context) { c.Set("id", user.Id); RequestAlipayF2F(c) })

	request := func(path, body, contentType string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		r.Header.Set("Content-Type", contentType)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	reset := func() {
		require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Update("quota", 0).Error)
		require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", order.Id).Updates(map[string]any{"status": "pending", "payment_provider": model.PaymentProviderAlipayF2F, "user_id": user.Id}).Error)
	}
	quota := func() int { var u model.User; require.NoError(t, db.First(&u, user.Id).Error); return u.Quota }
	form := func() url.Values {
		return url.Values{"app_id": {"app-test"}, "seller_id": {"seller-test"}, "trade_no": {"ALI202601"}, "out_trade_no": {order.TradeNo}, "trade_status": {"TRADE_SUCCESS"}, "total_amount": {"10.01"}, "sign_type": {"RSA2"}}
	}
	sign := func(values url.Values) {
		keys := make([]string, 0, len(values))
		for k := range values {
			if k != "sign" && k != "sign_type" && values.Get(k) != "" {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, k+"="+values.Get(k))
		}
		digest := sha256.Sum256([]byte(strings.Join(parts, "&")))
		sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		require.NoError(t, err)
		values.Set("sign", base64.StdEncoding.EncodeToString(sig))
	}

	t.Run("reject forged or mismatched callbacks without credit", func(t *testing.T) {
		cases := []struct {
			name, field, value string
			tamper             bool
		}{
			{"app", "app_id", "other-app", false}, {"seller", "seller_id", "other-seller", false},
			{"amount", "total_amount", "0.01", false}, {"fraction", "total_amount", "10.011", false},
			{"order", "out_trade_no", "unknown", false}, {"signature", "total_amount", "10.02", true},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				reset()
				values := form()
				if tc.tamper {
					sign(values)
					values.Set(tc.field, tc.value)
				} else {
					values.Set(tc.field, tc.value)
					sign(values)
				}
				w := request("/notify", values.Encode(), "application/x-www-form-urlencoded")
				assert.NotEqual(t, "success", w.Body.String())
				assert.Zero(t, quota())
			})
		}
		reset()
		values := form()
		sign(values)
		values.Add("total_amount", "10.01")
		assert.NotEqual(t, "success", request("/notify", values.Encode(), "application/x-www-form-urlencoded").Body.String())
		assert.Zero(t, quota())
	})
	t.Run("repeated callbacks credit once even when new payments disabled", func(t *testing.T) {
		reset()
		client.Enabled = false
		defer func() { client.Enabled = true }()
		values := form()
		sign(values)
		for range 2 {
			assert.Equal(t, "success", request("/notify", values.Encode(), "application/x-www-form-urlencoded").Body.String())
		}
		assert.Equal(t, 1000000, quota())
	})
	t.Run("nonpayment events never credit", func(t *testing.T) {
		reset()
		values := form()
		values.Set("trade_status", "WAIT_BUYER_PAY")
		sign(values)
		assert.Equal(t, "success", request("/notify", values.Encode(), "application/x-www-form-urlencoded").Body.String())
		assert.Zero(t, quota())
	})
	t.Run("query recovers missed notification and checks ownership amount and order", func(t *testing.T) {
		reset()
		body := `{"trade_no":"UTexisting"}`
		require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", order.Id).Update("user_id", user.Id+1).Error)
		assert.Contains(t, request("/query", body, "application/json").Body.String(), `"success":false`)
		assert.Zero(t, quota())
		reset()
		queryAmount = "9.99"
		assert.Contains(t, request("/query", body, "application/json").Body.String(), `"success":false`)
		assert.Zero(t, quota())
		queryAmount, queryOrder = "10.01", "other-order"
		assert.Contains(t, request("/query", body, "application/json").Body.String(), `"success":false`)
		assert.Zero(t, quota())
		queryOrder = ""
		assert.Contains(t, request("/query", body, "application/json").Body.String(), `"status":"success"`)
		assert.Equal(t, 1000000, quota())
		values := form()
		sign(values)
		assert.Equal(t, "success", request("/notify", values.Encode(), "application/x-www-form-urlencoded").Body.String())
		assert.Equal(t, 1000000, quota())
	})
	t.Run("wallet overflow rolls order back and provider mismatch is refused", func(t *testing.T) {
		reset()
		require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Update("quota", common.MaxWalletQuota).Error)
		require.Error(t, model.RechargeAlipayF2F(order.TradeNo, decimal.RequireFromString("10.01"), ""))
		assert.Equal(t, "pending", model.GetTopUpByTradeNo(order.TradeNo).Status)
		reset()
		require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", order.Id).Update("payment_provider", "epay").Error)
		require.ErrorIs(t, model.RechargeAlipayF2F(order.TradeNo, decimal.RequireFromString("10.01"), ""), model.ErrPaymentMethodMismatch)
		assert.Zero(t, quota())
	})
	t.Run("concurrent database settlements credit once without process mutex", func(t *testing.T) {
		reset()
		start := make(chan struct{})
		errs := make(chan error, 4)
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				<-start
				errs <- model.RechargeAlipayF2F(order.TradeNo, decimal.RequireFromString("10.01"), "")
			})
		}
		close(start)
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		assert.Equal(t, 1000000, quota())
	})
	t.Run("create snapshots rounded payment and requires compliance", func(t *testing.T) {
		cfg := operation_setting.GetPaymentSetting()
		old := *cfg
		oldPrice, oldMin := operation_setting.Price, operation_setting.MinTopUp
		defer func() { *cfg = old; operation_setting.Price, operation_setting.MinTopUp = oldPrice, oldMin }()
		cfg.ComplianceConfirmed = false
		assert.Contains(t, request("/pay", `{"amount":2}`, "application/json").Body.String(), `"message":"error"`)
		cfg.ComplianceConfirmed, cfg.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion
		operation_setting.Price, operation_setting.MinTopUp = 5.006, 1
		reset()
		w := request("/pay", `{"amount":2}`, "application/json")
		assert.Contains(t, w.Body.String(), `"amount":"10.01"`)
		assert.Contains(t, w.Body.String(), `https://qr.alipay.com/test-order`)
	})
	t.Run("SDK rejects unsigned query responses", func(t *testing.T) {
		badServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"alipay_trade_query_response":{"code":"10000","out_trade_no":"UTexisting","trade_no":"ALI","trade_status":"TRADE_SUCCESS","total_amount":"10.01"}}`))
		}))
		defer badServer.Close()
		badSDK, err := alipay.New("app-test", private, true, alipay.WithProductionGateway(badServer.URL))
		require.NoError(t, err)
		require.NoError(t, badSDK.LoadAliPayPublicKey(public))
		badClient := service.AlipayF2F{Client: badSDK}
		_, _, err = badClient.Query(context.Background(), order.TradeNo)
		require.Error(t, err)
	})
	// Verify malformed JSON does not create an order.
	before := int64(0)
	require.NoError(t, db.Model(&model.TopUp{}).Count(&before).Error)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/pay", bytes.NewBufferString(`{"amount":1e100}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	after := int64(0)
	require.NoError(t, db.Model(&model.TopUp{}).Count(&after).Error)
	assert.Equal(t, before, after)
}
