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
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/go-redis/redis/v8"
	"github.com/shopspring/decimal"
	"github.com/smartwalle/alipay/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// Exact rc.49 checkout schema, kept separate to exercise an existing installation.
type alipayRC49TopUp struct {
	Id              int
	UserId          int `gorm:"index"`
	Amount          int64
	Money           float64
	TradeNo         string `gorm:"unique;type:varchar(255);index"`
	PaymentMethod   string `gorm:"type:varchar(50)"`
	PaymentProvider string `gorm:"type:varchar(50);default:''"`
	CreateTime      int64
	CompleteTime    int64
	Status          string
}

// Pause the post-commit Redis increment at its network boundary and simulate
// a concurrent request hydrating a missing cache from the committed database.
type alipayCreditRaceHook struct {
	beforeCredit func()
}

func (h alipayCreditRaceHook) BeforeProcess(ctx context.Context, cmd redis.Cmder) (context.Context, error) {
	if cmd.Name() == "eval" && strings.Contains(fmt.Sprint(cmd.Args()[1]), "HINCRBY") {
		h.beforeCredit()
	}
	return ctx, nil
}
func (h alipayCreditRaceHook) AfterProcess(context.Context, redis.Cmder) error { return nil }
func (h alipayCreditRaceHook) BeforeProcessPipeline(ctx context.Context, _ []redis.Cmder) (context.Context, error) {
	return ctx, nil
}
func (h alipayCreditRaceHook) AfterProcessPipeline(context.Context, []redis.Cmder) error { return nil }

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
	// Fresh install and repeated startup, followed by an upgrade of the released
	// checkout schema with an existing order and its unique/index constraints.
	for range 2 {
		require.NoError(t, db.Table("alipay_fresh_topups").AutoMigrate(&model.TopUp{}))
	}
	require.True(t, db.Migrator().HasColumn("alipay_fresh_topups", "quota_snapshot"))
	require.NoError(t, db.Table("top_ups").AutoMigrate(&alipayRC49TopUp{}))
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}, &model.Option{}))
	user := model.User{Username: "utopia_alipay_test", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, db.Create(&user).Error)
	order := model.TopUp{UserId: user.Id, Amount: 2, Money: 10.01, TradeNo: "UTexisting", PaymentMethod: model.PaymentProviderAlipayF2F, PaymentProvider: model.PaymentProviderAlipayF2F, Status: common.TopUpStatusPending}
	legacy := alipayRC49TopUp{UserId: user.Id, Amount: order.Amount, Money: order.Money, TradeNo: order.TradeNo, PaymentMethod: order.PaymentMethod, PaymentProvider: order.PaymentProvider, Status: order.Status}
	require.NoError(t, db.Table("top_ups").Create(&legacy).Error)
	for range 2 {
		require.NoError(t, db.AutoMigrate(&model.TopUp{}))
	}
	require.NoError(t, db.Where("trade_no = ?", order.TradeNo).First(&order).Error)
	assert.Equal(t, legacy.Id, order.Id)
	assert.Equal(t, legacy.Amount, order.Amount)
	assert.Equal(t, legacy.Money, order.Money)
	assert.Zero(t, order.QuotaSnapshot)
	assert.True(t, db.Migrator().HasIndex(&model.TopUp{}, "idx_top_ups_user_id"))
	duplicate := order
	duplicate.Id = 0
	require.Error(t, duplicate.Insert(), "upgrade must preserve trade number uniqueness")
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
		// Public-key mode must reject certificate serials before the SDK tries a
		// network download; this client has no HTTP transport to fall back on.
		certificateValues := form()
		certificateValues.Set("alipay_cert_sn", "attacker-controlled")
		certificateClient := service.AlipayF2F{AppID: client.AppID, SellerID: client.SellerID}
		require.ErrorContains(t, certificateClient.VerifyNotification(t.Context(), certificateValues), "certificate mode")
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
	t.Run("callbacks credit the order owner regardless of caller identity", func(t *testing.T) {
		reset()
		other := model.User{Username: "alipay_other_user", Status: common.UserStatusEnabled, Group: "default", AffCode: "alipay-other"}
		require.NoError(t, db.Create(&other).Error)
		require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", order.Id).Update("user_id", other.Id).Error)
		assert.Contains(t, request("/query", `{"trade_no":"UTexisting","user_id":`+fmt.Sprint(other.Id)+`}`, "application/json").Body.String(), `"success":false`)
		values := form()
		sign(values)
		assert.Equal(t, "success", request("/notify", values.Encode(), "application/x-www-form-urlencoded").Body.String())
		assert.Zero(t, quota())
		require.NoError(t, db.First(&other, other.Id).Error)
		assert.Equal(t, 1000000, other.Quota)
		reset()
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
	t.Run("Redis hydration after commit must not apply the same credit twice", func(t *testing.T) {
		reset()
		require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Update("auth_version", 1).Error)
		// CI supplies a dedicated, disposable real Redis database. Locally the
		// same Lua/expiration interleaving runs against miniredis.
		redisAddress := os.Getenv("ALIPAY_TEST_REDIS_ADDR")
		if redisAddress == "" {
			redisAddress = miniredis.RunT(t).Addr()
		}
		redisClient := redis.NewClient(&redis.Options{Addr: redisAddress})
		defer redisClient.Close()
		previousRDB, previousEnabled := common.RDB, common.RedisEnabled
		previousSecret := common.SessionSecret
		common.RDB, common.RedisEnabled, common.SessionSecret = redisClient, true, "isolated-alipay-cache-test"
		defer func() {
			common.RDB, common.RedisEnabled, common.SessionSecret = previousRDB, previousEnabled, previousSecret
		}()
		replaceCache := false
		replaced := 0
		redisClient.AddHook(alipayCreditRaceHook{beforeCredit: func() {
			if replaceCache {
				// A new cache hydrated after commit already contains this credit.
				require.NoError(t, redisClient.Del(t.Context(), fmt.Sprintf("user:%d", user.Id)).Err())
				_, err := model.GetUserCache(user.Id)
				require.NoError(t, err)
				replaced++
			}
		}})
		for _, scenario := range []string{"missing", "existing", "replaced"} {
			reset()
			require.NoError(t, redisClient.FlushDB(t.Context()).Err())
			if scenario != "missing" {
				_, err := model.GetUserCache(user.Id)
				require.NoError(t, err)
			}
			replaceCache = scenario == "replaced"
			require.NoError(t, model.RechargeAlipayF2F(order.TradeNo, decimal.RequireFromString("10.01"), ""))
			cached, err := model.GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, 1000000, quota(), scenario)
			assert.Equal(t, 1000000, cached.Quota, scenario)
		}
		assert.Equal(t, 1, replaced, "test must reach the post-commit cache replacement race")
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
		var response struct {
			Data struct {
				TradeNo string `json:"trade_no"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
		require.NotEmpty(t, response.Data.TradeNo)
		oldQuotaPerUnit := common.QuotaPerUnit
		common.QuotaPerUnit *= 10
		defer func() { common.QuotaPerUnit = oldQuotaPerUnit }()
		require.NoError(t, model.RechargeAlipayF2F(response.Data.TradeNo, decimal.RequireFromString("10.01"), ""))
		assert.Equal(t, 1000000, quota(), "changing the conversion rate must not multiply an existing order's credit")
		// The administrator's explicit manual-completion feature must use the
		// same frozen quota, including when raced with the normal callback.
		require.NoError(t, db.Model(&model.TopUp{}).Where("trade_no = ?", response.Data.TradeNo).Update("status", "pending").Error)
		require.NoError(t, db.Model(&model.User{}).Where("id = ?", user.Id).Update("quota", 0).Error)
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		wg.Go(func() { errs <- model.ManualCompleteTopUp(response.Data.TradeNo, "") })
		wg.Go(func() { errs <- model.RechargeAlipayF2F(response.Data.TradeNo, decimal.RequireFromString("10.01"), "") })
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		assert.Equal(t, 1000000, quota())
	})
	t.Run("token checkout preserves fractional currency units and ignores injected credit fields", func(t *testing.T) {
		reset()
		cfg := operation_setting.GetPaymentSetting()
		old, oldPrice, oldMin := *cfg, operation_setting.Price, operation_setting.MinTopUp
		general := operation_setting.GetGeneralSetting()
		oldDisplay := general.QuotaDisplayType
		defer func() {
			*cfg, operation_setting.Price, operation_setting.MinTopUp = old, oldPrice, oldMin
			general.QuotaDisplayType = oldDisplay
		}()
		cfg.ComplianceConfirmed, cfg.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion
		operation_setting.Price, operation_setting.MinTopUp = 4.004, 1
		general.QuotaDisplayType = operation_setting.QuotaDisplayTypeTokens
		w := request("/pay", `{"amount":1250000,"user_id":999999,"quota_snapshot":9007199254740991,"money":0.01,"payment_provider":"epay","trade_no":"forged"}`, "application/json")
		require.Contains(t, w.Body.String(), `"amount":"10.01"`)
		var response struct {
			Data struct {
				TradeNo string `json:"trade_no"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(w.Body.Bytes(), &response))
		require.NotEmpty(t, response.Data.TradeNo)
		created := model.GetTopUpByTradeNo(response.Data.TradeNo)
		require.NotNil(t, created)
		assert.Equal(t, user.Id, created.UserId)
		assert.NotEqual(t, "forged", created.TradeNo)
		assert.Equal(t, model.PaymentProviderAlipayF2F, created.PaymentProvider)
		assert.EqualValues(t, 1250000, created.QuotaSnapshot)
		require.NoError(t, model.RechargeAlipayF2F(created.TradeNo, decimal.RequireFromString("10.01"), ""))
		assert.Equal(t, 1250000, quota())
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
	t.Run("admin configuration persists, redacts secrets and applies without restart", func(t *testing.T) {
		common.OptionMapRWMutex.Lock()
		oldOptions := common.OptionMap
		common.OptionMap = map[string]string{"ServerAddress": "https://payments.example"}
		common.OptionMapRWMutex.Unlock()
		defer func() {
			common.OptionMapRWMutex.Lock()
			common.OptionMap = oldOptions
			common.OptionMapRWMutex.Unlock()
		}()
		require.NoError(t, db.Where(&model.Option{Key: model.AlipayF2FOptionKey}).Delete(&model.Option{}).Error)
		// Existing test orders belong to the earlier isolated settlement scenarios.
		require.NoError(t, db.Model(&model.TopUp{}).Where("payment_provider = ?", model.PaymentProviderAlipayF2F).Update("status", common.TopUpStatusSuccess).Error)
		cfg := operation_setting.GetPaymentSetting()
		oldConfig := *cfg
		defer func() { *cfg = oldConfig }()
		cfg.ComplianceConfirmed, cfg.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion
		settingsRouter := gin.New()
		settingsRouter.GET("/settings", GetAlipayF2FSettings)
		settingsRouter.PUT("/settings", SaveAlipayF2FSettings)
		settingsRouter.GET("/options", GetOptions)
		settingsRouter.PUT("/options", UpdateOption)
		settingsRouter.GET("/protected", middleware.RootAuth(), GetAlipayF2FSettings)
		send := func(method, path string, value any) *httptest.ResponseRecorder {
			encoded, err := common.Marshal(value)
			require.NoError(t, err)
			request := httptest.NewRequest(method, path, bytes.NewReader(encoded))
			request.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			settingsRouter.ServeHTTP(w, request)
			return w
		}
		response := send(http.MethodGet, "/settings", nil)
		var initial struct {
			Data struct {
				Revision  string `json:"revision"`
				NotifyURL string `json:"notify_url"`
			} `json:"data"`
		}
		require.NoError(t, common.Unmarshal(response.Body.Bytes(), &initial))
		assert.Equal(t, "https://payments.example/api/alipay-f2f/notify", initial.Data.NotifyURL)
		assert.Equal(t, http.StatusUnauthorized, send(http.MethodGet, "/protected", nil).Code)
		request := alipayF2FSettingsRequest{AlipayF2FConfig: service.AlipayF2FConfig{Enabled: true, AppID: "app-test", SellerID: "seller-test", NotifyURL: initial.Data.NotifyURL, PrivateKey: private, AlipayPublicKey: public}, Revision: initial.Data.Revision}
		response = send(http.MethodPut, "/settings", request)
		require.Contains(t, response.Body.String(), `"success":true`)
		assert.NotContains(t, response.Body.String(), "PRIVATE KEY")
		assert.NotContains(t, response.Body.String(), "PUBLIC KEY")
		live, err := service.GetAlipayF2F()
		require.NoError(t, err)
		assert.True(t, live.Enabled)
		var persisted model.Option
		require.NoError(t, db.Where(&model.Option{Key: model.AlipayF2FOptionKey}).First(&persisted).Error)
		assert.Contains(t, persisted.Value, "PRIVATE KEY")
		assert.NotContains(t, send(http.MethodGet, "/settings", nil).Body.String(), "PRIVATE KEY")
		assert.NotContains(t, send(http.MethodGet, "/options", nil).Body.String(), model.AlipayF2FOptionKey)
		assert.Contains(t, send(http.MethodPut, "/options", OptionUpdateRequest{Key: model.AlipayF2FOptionKey, Value: "{}"}).Body.String(), `"success":false`)
		require.Error(t, model.UpdateOptionsBulk(map[string]string{model.AlipayF2FOptionKey: "{}"}))
		// Stale clients cannot overwrite a newer configuration.
		assert.Contains(t, send(http.MethodPut, "/settings", request).Body.String(), `"success":false`)
		request.Revision = fmt.Sprintf("%x", sha256.Sum256([]byte(persisted.Value)))
		request.PrivateKey, request.AlipayPublicKey = "", ""
		request.Enabled = false
		require.Contains(t, send(http.MethodPut, "/settings", request).Body.String(), `"success":true`)
		live, err = service.GetAlipayF2F()
		require.NoError(t, err)
		assert.False(t, live.Enabled)
		// Disabled new payments still have valid verification keys for old callbacks.
		values := form()
		sign(values)
		require.NoError(t, live.VerifyNotification(context.Background(), values))
		require.NoError(t, db.Where(&model.Option{Key: model.AlipayF2FOptionKey}).First(&persisted).Error)
		var stored service.AlipayF2FConfig
		require.NoError(t, common.UnmarshalJsonStr(persisted.Value, &stored))
		assert.Equal(t, strings.TrimSpace(private), stored.PrivateKey)
		assert.Equal(t, strings.TrimSpace(public), stored.AlipayPublicKey)
		request.Revision = fmt.Sprintf("%x", sha256.Sum256([]byte(persisted.Value)))
		before := persisted.Value
		request.PrivateKey = "invalid-key"
		assert.Contains(t, send(http.MethodPut, "/settings", request).Body.String(), `"success":false`)
		request.PrivateKey = ""
		request.NotifyURL = "http://payments.example/api/alipay-f2f/notify"
		assert.Contains(t, send(http.MethodPut, "/settings", request).Body.String(), `"success":false`)
		require.NoError(t, db.Where(&model.Option{Key: model.AlipayF2FOptionKey}).First(&persisted).Error)
		assert.Equal(t, before, persisted.Value)
		// Changing merchant/environment with a pending order is rejected atomically.
		require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", order.Id).Update("status", common.TopUpStatusPending).Error)
		request.NotifyURL, request.SellerID = stored.NotifyURL, "another-seller"
		assert.Contains(t, send(http.MethodPut, "/settings", request).Body.String(), "Settle pending Alipay orders")
		require.NoError(t, db.Where(&model.Option{Key: model.AlipayF2FOptionKey}).First(&persisted).Error)
		assert.Equal(t, before, persisted.Value)
		// Exercise the real authorization middleware with live login sessions,
		// rather than trusting a forged role/user-id header or request body.
		require.NoError(t, db.AutoMigrate(&model.UserSession{}, &model.AuditLog{}))
		oldSecret := common.SessionSecret
		common.SessionSecret = "isolated-alipay-security-test-secret"
		defer func() { common.SessionSecret = oldSecret }()
		protected := gin.New()
		protected.GET("/api/option/alipay-f2f", middleware.RootAuth(), GetAlipayF2FSettings)
		protected.PUT("/api/option/alipay-f2f", middleware.RootAuth(), SaveAlipayF2FSettings)
		protected.POST("/api/option/alipay-f2f/check", middleware.RootAuth(), CheckAlipayF2FSettings)
		protected.POST("/api/user/topup/complete", middleware.AdminAuth(), AdminCompleteTopUp)
		for _, role := range []int{common.RoleCommonUser, common.RoleAdminUser, common.RoleRootUser} {
			actor := model.User{Username: fmt.Sprintf("alipay_role_%d", role), Role: role, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: fmt.Sprintf("alipay-role-%d", role)}
			require.NoError(t, db.Create(&actor).Error)
			bundle, err := service.CreateLoginSession(actor.Id, "password", "127.0.0.1", "alipay-security-test")
			require.NoError(t, err)
			for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodPost} {
				path := "/api/option/alipay-f2f"
				if method == http.MethodPost {
					path += "/check"
				}
				if role == common.RoleRootUser && method != http.MethodGet {
					continue
				}
				r := httptest.NewRequest(method, path, strings.NewReader(`{}`))
				r.Header.Set("Authorization", "Bearer "+bundle.AccessToken)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("New-Api-User", "1")
				r.Header.Set("role", fmt.Sprint(common.RoleRootUser))
				w := httptest.NewRecorder()
				protected.ServeHTTP(w, r)
				if role == common.RoleRootUser {
					assert.Equal(t, http.StatusOK, w.Code)
					assert.Contains(t, w.Body.String(), `"private_key_configured":true`)
				} else {
					assert.Equal(t, http.StatusForbidden, w.Code)
				}
				assert.NotContains(t, w.Body.String(), private)
				assert.NotContains(t, w.Body.String(), "PRIVATE KEY")
			}
			if role == common.RoleCommonUser {
				r := httptest.NewRequest(http.MethodPost, "/api/user/topup/complete", strings.NewReader(`{"trade_no":"UTexisting"}`))
				r.Header.Set("Authorization", "Bearer "+bundle.AccessToken)
				r.Header.Set("Content-Type", "application/json")
				w := httptest.NewRecorder()
				protected.ServeHTTP(w, r)
				assert.Equal(t, http.StatusForbidden, w.Code)
			}
		}
		// Legacy mounted-file configuration can be imported with blank key inputs.
		keyDir := t.TempDir()
		privatePath, publicPath := filepath.Join(keyDir, "private.pem"), filepath.Join(keyDir, "public.pem")
		require.NoError(t, os.WriteFile(privatePath, []byte(private), 0600))
		require.NoError(t, os.WriteFile(publicPath, []byte(public), 0600))
		for name, value := range map[string]string{"ALIPAY_F2F_APP_ID": "app-test", "ALIPAY_F2F_SELLER_ID": "seller-test", "ALIPAY_F2F_NOTIFY_URL": stored.NotifyURL, "ALIPAY_F2F_PRIVATE_KEY_FILE": privatePath, "ALIPAY_F2F_PUBLIC_KEY_FILE": publicPath, "ALIPAY_F2F_ENABLED": "false"} {
			t.Setenv(name, value)
		}
		require.NoError(t, db.Where(&model.Option{Key: model.AlipayF2FOptionKey}).Delete(&model.Option{}).Error)
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, model.AlipayF2FOptionKey)
		common.OptionMapRWMutex.Unlock()
		live, err = service.GetAlipayF2F()
		require.NoError(t, err)
		assert.False(t, live.Enabled)
		assert.Contains(t, send(http.MethodGet, "/settings", nil).Body.String(), `"from_environment":true`)
		request.SellerID, request.Revision = "seller-test", fmt.Sprintf("%x", sha256.Sum256(nil))
		require.Contains(t, send(http.MethodPut, "/settings", request).Body.String(), `"success":true`)
		t.Setenv("ALIPAY_F2F_ENABLED", "true")
		t.Setenv("ALIPAY_F2F_PRIVATE_KEY_FILE", "missing-file")
		live, err = service.GetAlipayF2F()
		require.NoError(t, err)
		assert.False(t, live.Enabled, "database disable must override old environment settings")

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
