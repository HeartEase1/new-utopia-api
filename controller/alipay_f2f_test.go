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
	"errors"
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
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/middleware"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
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
	require.True(t, db.Migrator().HasColumn("alipay_fresh_topups", "expires_at"))
	require.NoError(t, db.Table("top_ups").AutoMigrate(&alipayRC49TopUp{}))
	require.NoError(t, db.AutoMigrate(&model.User{}, &model.Log{}, &model.Option{}, &model.SystemTask{}))
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
	assert.Zero(t, order.ExpiresAt)
	assert.Zero(t, order.ReferralUserID)
	assert.Zero(t, order.ReferralQuota)
	assert.True(t, db.Migrator().HasColumn(&model.TopUp{}, "expires_at"))
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
	var precreateBusinessParams map[string]string
	closeResult, closeCalls := "ACQ.TRADE_NOT_EXIST", 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		var biz struct {
			BusinessParams       map[string]string `json:"business_params"`
			OutTradeNo           string            `json:"out_trade_no"`
			TotalAmount          string            `json:"total_amount"`
			SellerID             string            `json:"seller_id"`
			TimeoutExpress       string            `json:"timeout_express"`
			TimeExpire           string            `json:"time_expire"`
			QRCodeTimeoutExpress string            `json:"qr_code_timeout_express"`
		}
		require.NoError(t, common.UnmarshalJsonStr(r.Form.Get("biz_content"), &biz))
		responseName := "alipay_trade_query_response"
		tradeNo := biz.OutTradeNo
		if queryOrder != "" {
			tradeNo = queryOrder
		}
		body := map[string]any{"code": "10000", "trade_no": "ALI202601", "out_trade_no": tradeNo, "total_amount": queryAmount, "trade_status": queryStatus}
		if queryStatus == "NOT_EXIST" {
			body = map[string]any{"code": "40004", "sub_code": "ACQ.TRADE_NOT_EXIST"}
		}
		if r.Form.Get("method") == "alipay.trade.close" {
			closeCalls++
			responseName = "alipay_trade_close_response"
			body = map[string]any{"code": "40004", "sub_code": closeResult}
			if closeResult == "CLOSED" {
				body = map[string]any{"code": "10000", "out_trade_no": tradeNo, "trade_no": "ALI202601"}
			}
		}
		if r.Form.Get("method") == "alipay.trade.precreate" {
			precreateBusinessParams = biz.BusinessParams
			responseName = "alipay_trade_precreate_response"
			var saved model.TopUp
			require.NoError(t, db.Where("trade_no = ?", biz.OutTradeNo).First(&saved).Error)
			assert.Equal(t, "pending", saved.Status)
			assert.Equal(t, "10.01", biz.TotalAmount)
			assert.Equal(t, "seller-test", biz.SellerID)
			assert.Equal(t, "5m", biz.TimeoutExpress)
			assert.Equal(t, "5m", biz.QRCodeTimeoutExpress)
			assert.Equal(t, saved.CreateTime+300, saved.ExpiresAt)
			assert.Equal(t, time.Unix(saved.ExpiresAt, 0).In(time.FixedZone("CST", 8*60*60)).Format("2006-01-02 15:04:05"), biz.TimeExpire)
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
	router.POST("/cancel", func(c *gin.Context) { c.Set("id", user.Id); CancelAlipayF2FOrder(c) })

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
	t.Run("closed or never-paid orders expire and a late payment still credits once", func(t *testing.T) {
		reset()
		defer func() { queryStatus = "TRADE_SUCCESS" }()
		body := `{"trade_no":"UTexisting"}`
		createdAt := func(unix int64) {
			require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", order.Id).Update("create_time", unix).Error)
		}
		defer createdAt(0)
		// An unscanned QR code has no Alipay trade yet and can still be paid.
		queryStatus = "NOT_EXIST"
		createdAt(time.Now().Unix())
		assert.Contains(t, request("/query", body, "application/json").Body.String(), `"status":"pending"`)
		assert.Equal(t, "pending", model.GetTopUpByTradeNo(order.TradeNo).Status)
		createdAt(time.Now().Add(-4 * time.Hour).Unix())
		assert.Contains(t, request("/query", body, "application/json").Body.String(), `"status":"expired"`)
		assert.Equal(t, "expired", model.GetTopUpByTradeNo(order.TradeNo).Status)
		reset()
		createdAt(time.Now().Unix())
		queryStatus = "TRADE_CLOSED"
		assert.Contains(t, request("/query", body, "application/json").Body.String(), `"status":"expired"`)
		assert.Equal(t, "expired", model.GetTopUpByTradeNo(order.TradeNo).Status)
		assert.Zero(t, quota())
		reset()
		closed := form()
		closed.Set("trade_status", "TRADE_CLOSED")
		sign(closed)
		assert.Equal(t, "success", request("/notify", closed.Encode(), "application/x-www-form-urlencoded").Body.String())
		assert.Equal(t, "expired", model.GetTopUpByTradeNo(order.TradeNo).Status)
		paid := form()
		sign(paid)
		for range 2 {
			assert.Equal(t, "success", request("/notify", paid.Encode(), "application/x-www-form-urlencoded").Body.String())
		}
		assert.Equal(t, 1000000, quota())
		assert.Equal(t, "success", model.GetTopUpByTradeNo(order.TradeNo).Status)
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
	t.Run("cancel checks ownership and closes an unpaid trade without credit", func(t *testing.T) {
		reset()
		defer func() { closeResult = "ACQ.TRADE_NOT_EXIST"; queryOrder = "" }()
		closeResult = "CLOSED"
		body := `{"trade_no":"UTexisting"}`
		before := closeCalls
		require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", order.Id).Update("user_id", user.Id+1).Error)
		assert.Contains(t, request("/cancel", body, "application/json").Body.String(), `"success":false`)
		assert.Equal(t, before, closeCalls)
		reset()
		queryOrder = "another-trade"
		assert.Contains(t, request("/cancel", body, "application/json").Body.String(), `"success":false`)
		assert.Equal(t, "pending", model.GetTopUpByTradeNo(order.TradeNo).Status)
		queryOrder = ""
		for range 2 {
			assert.Contains(t, request("/cancel", body, "application/json").Body.String(), `"status":"expired"`)
		}
		assert.Zero(t, quota())
		paid := form()
		sign(paid)
		for range 2 {
			assert.Equal(t, "success", request("/notify", paid.Encode(), "application/x-www-form-urlencoded").Body.String())
		}
		assert.Equal(t, 1000000, quota(), "a delayed authenticated payment must still settle exactly once")
	})
	t.Run("payment wins cancellation and gateway failure never marks unpaid trade closed", func(t *testing.T) {
		reset()
		defer func() { closeResult = "ACQ.TRADE_NOT_EXIST"; queryStatus = "TRADE_SUCCESS" }()
		closeResult, queryStatus = "ACQ.TRADE_STATUS_ERROR", "TRADE_SUCCESS"
		body := `{"trade_no":"UTexisting"}`
		for range 2 {
			assert.Contains(t, request("/cancel", body, "application/json").Body.String(), `"status":"success"`)
		}
		assert.Equal(t, 1000000, quota())
		reset()
		queryStatus = "WAIT_BUYER_PAY"
		assert.Contains(t, request("/cancel", body, "application/json").Body.String(), `"success":false`)
		assert.Equal(t, "pending", model.GetTopUpByTradeNo(order.TradeNo).Status)
		assert.Zero(t, quota())
	})
	t.Run("unscanned QR is retained until its fixed deadline and expires in the leased background task", func(t *testing.T) {
		reset()
		defer func() {
			queryStatus = "TRADE_SUCCESS"
			require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", order.Id).Update("expires_at", 0).Error)
		}()
		queryStatus = "NOT_EXIST"
		require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", order.Id).Update("expires_at", time.Now().Add(time.Minute).Unix()).Error)
		assert.Contains(t, request("/cancel", `{"trade_no":"UTexisting"}`, "application/json").Body.String(), `"status":"pending"`)
		assert.Zero(t, quota())
		require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", order.Id).Update("expires_at", time.Now().Add(-time.Second).Unix()).Error)
		require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))
		task, err := model.CreateSystemTask(alipayF2FExpiryTaskType, alipayF2FExpiryProgress{}, nil)
		require.NoError(t, err)
		claimed, ok, err := model.ClaimSystemTask(task.ID, task.Type, "expiry-test", time.Now().Add(time.Minute).Unix())
		require.NoError(t, err)
		require.True(t, ok)
		client.Enabled = false // disabling checkout must not disable reconciliation
		defer func() { client.Enabled = true }()
		handler := alipayF2FExpiryHandler{}
		require.True(t, handler.Enabled())
		handler.Run(t.Context(), claimed, "expiry-test")
		finished, err := model.GetSystemTaskByTaskID(task.TaskID)
		require.NoError(t, err)
		assert.Equal(t, model.SystemTaskStatusSucceeded, finished.Status)
		assert.Equal(t, "expired", model.GetTopUpByTradeNo(order.TradeNo).Status)
		assert.Zero(t, quota())
		progress, err := reconcileAlipayF2FExpiredOrders(t.Context(), order.Id)
		require.NoError(t, err)
		assert.Zero(t, progress.LastID, "cursor wraps so failed old orders will be retried")
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
			require.NoError(t, db.Model(&user).Update("aff_quota", 500000).Error)
			require.NoError(t, user.TransferAffQuotaToQuota(500000))
			cached, err = model.GetUserCache(user.Id)
			require.NoError(t, err)
			assert.Equal(t, 1500000, quota(), "referral transfer: "+scenario)
			assert.Equal(t, 1500000, cached.Quota, "referral transfer: "+scenario)
		}
		assert.Equal(t, 2, replaced, "both payment and referral transfer must reach the post-commit cache replacement race")
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
		assert.Nil(t, precreateBusinessParams, "notification opt-in is absent by default")
		created := model.GetTopUpByTradeNo(response.Data.TradeNo)
		require.NotNil(t, created)
		client.NotifyAlipayApp = true
		t.Cleanup(func() { client.NotifyAlipayApp = false })
		_, err := client.Precreate(context.Background(), created.TradeNo, "10.01", time.Unix(created.ExpiresAt, 0))
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"notify_alipay_app": "true"}, precreateBusinessParams)
		client.NotifyAlipayApp = false
		_, err = client.Precreate(context.Background(), created.TradeNo, "10.01", time.Unix(created.ExpiresAt, 0))
		require.NoError(t, err)
		assert.Nil(t, precreateBusinessParams, "turning notifications off omits the opt-in again")
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
			require.NoError(t, r.ParseForm())
			if r.Form.Get("method") == "alipay.trade.close" {
				_, _ = w.Write([]byte(`{"alipay_trade_close_response":{"code":"10000","out_trade_no":"UTexisting","trade_no":"ALI"}}`))
				return
			}
			_, _ = w.Write([]byte(`{"alipay_trade_query_response":{"code":"10000","out_trade_no":"UTexisting","trade_no":"ALI","trade_status":"TRADE_SUCCESS","total_amount":"10.01"}}`))
		}))
		defer badServer.Close()
		badSDK, err := alipay.New("app-test", private, true, alipay.WithProductionGateway(badServer.URL))
		require.NoError(t, err)
		require.NoError(t, badSDK.LoadAliPayPublicKey(public))
		badClient := service.AlipayF2F{Client: badSDK}
		_, _, err = badClient.Query(context.Background(), order.TradeNo)
		require.Error(t, err)
		closed, err := badClient.Close(context.Background(), order.TradeNo)
		require.Error(t, err)
		assert.False(t, closed)
	})
	t.Run("failed configuration write cleans only its new key file and unsafe paths fail closed", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("ALIPAY_F2F_KEY_DIR", dir)
		config := service.AlipayF2FConfig{PrivateKey: private, KeyStorage: service.AlipayF2FKeyStorageFile}
		encoded, finish, err := service.SealAlipayF2FConfig(config, "")
		require.NoError(t, err)
		assert.NotContains(t, encoded, "PRIVATE KEY")
		files, err := filepath.Glob(filepath.Join(dir, "*.pem"))
		require.NoError(t, err)
		require.Len(t, files, 1)
		err = model.SaveAlipayF2FOption("stale-revision", encoded, false)
		require.Error(t, err)
		finish(false)
		_, err = os.Stat(files[0])
		require.True(t, os.IsNotExist(err))
		common.OptionMapRWMutex.Lock()
		previous := common.OptionMap
		common.OptionMap = map[string]string{model.AlipayF2FOptionKey: `{"private_key_file":"../outside.pem"}`}
		common.OptionMapRWMutex.Unlock()
		defer func() {
			common.OptionMapRWMutex.Lock()
			common.OptionMap = previous
			common.OptionMapRWMutex.Unlock()
		}()
		_, _, err = service.LoadAlipayF2FConfig()
		require.ErrorIs(t, err, service.ErrAlipayF2FPrivateKeyUnavailable)
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
		storeDir := t.TempDir()
		t.Setenv("ALIPAY_F2F_KEY_DIR", storeDir)
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
		request := alipayF2FSettingsRequest{AlipayF2FConfig: service.AlipayF2FConfig{Enabled: true, NotifyAlipayApp: true, AppID: "app-test", SellerID: "seller-test", NotifyURL: initial.Data.NotifyURL, PrivateKey: private, AlipayPublicKey: public}, Revision: initial.Data.Revision}
		response = send(http.MethodPut, "/settings", request)
		require.Contains(t, response.Body.String(), `"success":true`)
		assert.NotContains(t, response.Body.String(), "PRIVATE KEY")
		assert.NotContains(t, response.Body.String(), "PUBLIC KEY")
		live, err := service.GetAlipayF2F()
		require.NoError(t, err)
		assert.True(t, live.Enabled)
		assert.True(t, live.NotifyAlipayApp)
		var persisted model.Option
		require.NoError(t, db.Where(&model.Option{Key: model.AlipayF2FOptionKey}).First(&persisted).Error)
		// The default storage keeps the key in a server file; the database
		// holds only the file reference.
		assert.Contains(t, response.Body.String(), `"notify_alipay_app":true`)
		assert.Contains(t, persisted.Value, `"notify_alipay_app":true`)
		assert.Contains(t, response.Body.String(), `"key_storage":"file"`)
		assert.NotContains(t, persisted.Value, "PRIVATE KEY")
		keyFiles, err := filepath.Glob(filepath.Join(storeDir, "private-*.pem"))
		require.NoError(t, err)
		require.Len(t, keyFiles, 1)
		savedKey, err := os.ReadFile(keyFiles[0])
		require.NoError(t, err)
		assert.Equal(t, strings.TrimSpace(private), string(savedKey))
		assert.NotContains(t, send(http.MethodGet, "/settings", nil).Body.String(), "PRIVATE KEY")
		assert.NotContains(t, send(http.MethodGet, "/options", nil).Body.String(), model.AlipayF2FOptionKey)
		assert.Contains(t, send(http.MethodPut, "/options", OptionUpdateRequest{Key: model.AlipayF2FOptionKey, Value: "{}"}).Body.String(), `"success":false`)
		require.Error(t, model.UpdateOptionsBulk(map[string]string{model.AlipayF2FOptionKey: "{}"}))
		// Stale clients cannot overwrite a newer configuration.
		assert.Contains(t, send(http.MethodPut, "/settings", request).Body.String(), `"success":false`)
		var syncTasks []model.SystemTask
		require.NoError(t, db.Where("type = ?", model.SyncTaskType(model.SyncTopicOptions)).Find(&syncTasks).Error)
		require.Len(t, syncTasks, 1, "successful Alipay save must notify other nodes; rejected writes must not")
		assert.NotContains(t, syncTasks[0].Payload, "PRIVATE KEY")
		// Simulate another node with an empty option cache applying the reload.
		common.OptionMapRWMutex.Lock()
		delete(common.OptionMap, model.AlipayF2FOptionKey)
		common.OptionMapRWMutex.Unlock()
		require.NoError(t, model.LoadOptionsFromDatabase())
		live, err = service.GetAlipayF2F()
		require.NoError(t, err)
		assert.True(t, live.Enabled)
		assert.True(t, live.NotifyAlipayApp)
		request.Revision = fmt.Sprintf("%x", sha256.Sum256([]byte(persisted.Value)))
		request.PrivateKey, request.AlipayPublicKey = "", ""
		request.Enabled = false
		request.NotifyAlipayApp = false
		require.Contains(t, send(http.MethodPut, "/settings", request).Body.String(), `"success":true`)
		live, err = service.GetAlipayF2F()
		require.NoError(t, err)
		assert.False(t, live.Enabled)
		assert.False(t, live.NotifyAlipayApp)
		assert.Contains(t, send(http.MethodGet, "/settings", nil).Body.String(), `"notify_alipay_app":false`)
		// Disabled new payments still have valid verification keys for old callbacks.
		values := form()
		sign(values)
		require.NoError(t, live.VerifyNotification(context.Background(), values))
		require.NoError(t, db.Where(&model.Option{Key: model.AlipayF2FOptionKey}).First(&persisted).Error)
		var stored service.AlipayF2FConfig
		require.NoError(t, common.UnmarshalJsonStr(persisted.Value, &stored))
		assert.Empty(t, stored.PrivateKey)
		assert.Equal(t, strings.TrimSpace(public), stored.AlipayPublicKey)
		loaded, _, err := service.LoadAlipayF2FConfig()
		require.NoError(t, err)
		assert.Equal(t, strings.TrimSpace(private), loaded.PrivateKey)
		unchangedFiles, err := filepath.Glob(filepath.Join(storeDir, "private-*.pem"))
		require.NoError(t, err)
		assert.Equal(t, keyFiles, unchangedFiles, "a blank key input must keep the existing key file")
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
		require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", order.Id).Updates(map[string]any{"status": common.TopUpStatusPending, "create_time": time.Now().Unix()}).Error)
		request.NotifyURL, request.SellerID = stored.NotifyURL, "another-seller"
		assert.Contains(t, send(http.MethodPut, "/settings", request).Body.String(), "Settle pending Alipay orders")
		require.NoError(t, db.Where(&model.Option{Key: model.AlipayF2FOptionKey}).First(&persisted).Error)
		assert.Equal(t, before, persisted.Value)
		// An abandoned order that Alipay never saw is expired and stops blocking.
		require.NoError(t, db.Model(&model.TopUp{}).Where("id = ?", order.Id).Update("create_time", 0).Error)
		queryStatus = "NOT_EXIST"
		response = send(http.MethodPut, "/settings", request)
		queryStatus = "TRADE_SUCCESS"
		require.Contains(t, response.Body.String(), `"success":true`)
		assert.Equal(t, common.TopUpStatusExpired, model.GetTopUpByTradeNo(order.TradeNo).Status)
		require.NoError(t, db.Where(&model.Option{Key: model.AlipayF2FOptionKey}).First(&persisted).Error)
		request.Revision = fmt.Sprintf("%x", sha256.Sum256([]byte(persisted.Value)))
		// Encrypted storage requires an explicit CRYPTO_SECRET, leaves only
		// ciphertext in the database. Old files remain for stale node snapshots.
		request.SellerID, request.KeyStorage = "seller-test", service.AlipayF2FKeyStorageEncrypted
		t.Setenv("CRYPTO_SECRET", "")
		assert.Contains(t, send(http.MethodPut, "/settings", request).Body.String(), "CRYPTO_SECRET")
		oldCryptoSecret := common.CryptoSecret
		defer func() { common.CryptoSecret = oldCryptoSecret }()
		t.Setenv("CRYPTO_SECRET", "alipay-test-crypto-secret")
		common.CryptoSecret = "alipay-test-crypto-secret"
		response = send(http.MethodPut, "/settings", request)
		require.Contains(t, response.Body.String(), `"success":true`)
		assert.Contains(t, response.Body.String(), `"key_storage":"encrypted"`)
		require.NoError(t, db.Where(&model.Option{Key: model.AlipayF2FOptionKey}).First(&persisted).Error)
		assert.NotContains(t, persisted.Value, "PRIVATE KEY")
		assert.Contains(t, persisted.Value, "private_key_encrypted")
		keyFiles, err = filepath.Glob(filepath.Join(storeDir, "private-*.pem"))
		require.NoError(t, err)
		require.Len(t, keyFiles, 1)
		loaded, _, err = service.LoadAlipayF2FConfig()
		require.NoError(t, err)
		assert.Equal(t, strings.TrimSpace(private), loaded.PrivateKey)
		// Ciphertext authentication must reject corruption without exposing a key.
		var encrypted map[string]any
		require.NoError(t, common.UnmarshalJsonStr(persisted.Value, &encrypted))
		ciphertext := encrypted["private_key_encrypted"].(string)
		sealed, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(ciphertext, "v1."))
		require.NoError(t, err)
		sealed[len(sealed)-1] ^= 1
		encrypted["private_key_encrypted"] = "v1." + base64.StdEncoding.EncodeToString(sealed)
		corrupt, err := common.Marshal(encrypted)
		require.NoError(t, err)
		common.OptionMapRWMutex.Lock()
		common.OptionMap[model.AlipayF2FOptionKey] = string(corrupt)
		common.OptionMapRWMutex.Unlock()
		_, _, err = service.LoadAlipayF2FConfig()
		require.ErrorIs(t, err, service.ErrAlipayF2FPrivateKeyUnavailable)
		common.OptionMapRWMutex.Lock()
		common.OptionMap[model.AlipayF2FOptionKey] = persisted.Value
		common.OptionMapRWMutex.Unlock()
		// A node holding a different secret reports the key as unavailable.
		common.CryptoSecret = "another-node-secret"
		_, err = service.GetAlipayF2F()
		require.ErrorIs(t, err, service.ErrAlipayF2FPrivateKeyUnavailable)
		unavailable := send(http.MethodGet, "/settings", nil).Body.String()
		assert.Contains(t, unavailable, `"private_key_unavailable":true`)
		assert.Contains(t, unavailable, `"private_key_configured":false`)
		common.CryptoSecret = "alipay-test-crypto-secret"
		// A configuration saved before key storage existed still works and its
		// plaintext key leaves the database on the next save.
		legacy, err := common.Marshal(map[string]any{"enabled": false, "app_id": "app-test", "seller_id": "seller-test", "notify_url": stored.NotifyURL, "private_key": private, "alipay_public_key": public})
		require.NoError(t, err)
		require.NoError(t, db.Save(&model.Option{Key: model.AlipayF2FOptionKey, Value: string(legacy)}).Error)
		common.OptionMapRWMutex.Lock()
		common.OptionMap[model.AlipayF2FOptionKey] = string(legacy)
		common.OptionMapRWMutex.Unlock()
		_, err = service.GetAlipayF2F()
		require.NoError(t, err)
		assert.Contains(t, send(http.MethodGet, "/settings", nil).Body.String(), `"key_storage":"database"`)
		request.KeyStorage, request.Revision = "", fmt.Sprintf("%x", sha256.Sum256(legacy))
		response = send(http.MethodPut, "/settings", request)
		require.Contains(t, response.Body.String(), `"success":true`)
		assert.Contains(t, response.Body.String(), `"key_storage":"file"`)
		require.NoError(t, db.Where(&model.Option{Key: model.AlipayF2FOptionKey}).First(&persisted).Error)
		assert.NotContains(t, persisted.Value, "PRIVATE KEY")
		// A lost key file is reported and repaired only by entering the key again.
		keyFiles, err = filepath.Glob(filepath.Join(storeDir, "private-*.pem"))
		require.NoError(t, err)
		require.Len(t, keyFiles, 2, "old node snapshots must keep working across a storage switch")
		require.NoError(t, db.Where(&model.Option{Key: model.AlipayF2FOptionKey}).First(&persisted).Error)
		var fileReference struct {
			Name string `json:"private_key_file"`
		}
		require.NoError(t, common.UnmarshalJsonStr(persisted.Value, &fileReference))
		require.NoError(t, os.Remove(filepath.Join(storeDir, fileReference.Name)))
		_, err = service.GetAlipayF2F()
		require.ErrorIs(t, err, service.ErrAlipayF2FPrivateKeyUnavailable)
		request.Revision = fmt.Sprintf("%x", sha256.Sum256([]byte(persisted.Value)))
		assert.Contains(t, send(http.MethodPut, "/settings", request).Body.String(), `"success":false`)
		request.PrivateKey = private
		require.Contains(t, send(http.MethodPut, "/settings", request).Body.String(), `"success":true`)
		request.PrivateKey = ""
		_, err = service.GetAlipayF2F()
		require.NoError(t, err)
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
		assert.False(t, live.NotifyAlipayApp)
		assert.Contains(t, send(http.MethodGet, "/settings", nil).Body.String(), `"notify_alipay_app":false`)
		assert.Contains(t, send(http.MethodGet, "/settings", nil).Body.String(), `"from_environment":true`)
		request.SellerID, request.Revision = "seller-test", fmt.Sprintf("%x", sha256.Sum256(nil))
		require.Contains(t, send(http.MethodPut, "/settings", request).Body.String(), `"success":true`)
		t.Setenv("ALIPAY_F2F_ENABLED", "true")
		t.Setenv("ALIPAY_F2F_PRIVATE_KEY_FILE", "missing-file")
		live, err = service.GetAlipayF2F()
		require.NoError(t, err)
		assert.False(t, live.Enabled, "database disable must override old environment settings")

	})

	t.Run("online topups award referral earnings once within the registration window", func(t *testing.T) {
		common.OptionMapRWMutex.Lock()
		oldOptions := common.OptionMap
		common.OptionMap = map[string]string{}
		common.OptionMapRWMutex.Unlock()
		defer func() {
			common.OptionMapRWMutex.Lock()
			common.OptionMap = oldOptions
			common.OptionMapRWMutex.Unlock()
		}()
		oldSettings := operation_setting.GetReferralSetting()
		oldPayment := *operation_setting.GetPaymentSetting()
		oldInviter := common.QuotaForInviter
		common.QuotaForInviter = 999999
		payment := operation_setting.GetPaymentSetting()
		payment.ComplianceConfirmed, payment.ComplianceTermsVersion = true, operation_setting.CurrentComplianceTermsVersion
		require.NoError(t, model.UpdateOption("referral_setting.percent", "12.5"))
		require.NoError(t, model.UpdateOption("referral_setting.days", "30"))
		defer func() {
			common.QuotaForInviter = oldInviter
			*payment = oldPayment
			require.NoError(t, model.UpdateOption("referral_setting.percent", fmt.Sprint(oldSettings.Percent)))
			require.NoError(t, model.UpdateOption("referral_setting.days", fmt.Sprint(oldSettings.Days)))
		}()
		for _, invalid := range []struct{ key, value string }{
			{"referral_setting.percent", "-1"}, {"referral_setting.percent", "101"},
			{"referral_setting.percent", "NaN"}, {"referral_setting.percent", "+Inf"},
			{"referral_setting.days", "0"}, {"referral_setting.days", "1.5"}, {"referral_setting.days", "3651"},
			{"QuotaForInviter", "500000"},
		} {
			require.Error(t, model.UpdateOption(invalid.key, invalid.value), "%s=%s", invalid.key, invalid.value)
		}
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("referral_setting_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "options" {
				tx.AddError(errors.New("injected referral settings failure"))
			}
		}))
		updateErr := model.UpdateOption("referral_setting.percent", "20")
		require.NoError(t, db.Callback().Update().Remove("referral_setting_failure"))
		require.Error(t, updateErr)
		assert.Equal(t, 12.5, operation_setting.GetReferralSetting().Percent)
		var savedOption model.Option
		require.NoError(t, db.Where(&model.Option{Key: "referral_setting.percent"}).First(&savedOption).Error)
		assert.Equal(t, "12.5", savedOption.Value)
		inviter := model.User{Username: "referrer", AffCode: "rfr1", Status: common.UserStatusEnabled, AffQuota: 700000, AffHistoryQuota: 700000}
		require.NoError(t, db.Create(&inviter).Error)
		invited := model.User{Username: "referred", AffCode: "rfd1", Status: common.UserStatusEnabled, InviterId: inviter.Id, CreatedAt: common.GetTimestamp() - 86400, Group: "default"}
		require.NoError(t, db.Create(&invited).Error)
		invited.FinishInsert(inviter.Id)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Equal(t, 1, inviter.AffCount)
		assert.Equal(t, 700000, inviter.AffQuota, "legacy inviter signup reward must no longer apply")
		oauthInvited := model.User{Username: "oauth_referred", AffCode: "rfd2", Status: common.UserStatusEnabled, InviterId: inviter.Id, Group: "default"}
		require.NoError(t, db.Create(&oauthInvited).Error)
		oauthInvited.FinalizeOAuthUserCreation(inviter.Id)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Equal(t, 2, inviter.AffCount)
		assert.Equal(t, 700000, inviter.AffQuota, "OAuth registration must not award the old bonus either")
		// Use one USD of credited quota for every provider, despite different units.
		providers := []string{model.PaymentProviderAlipayF2F, model.PaymentProviderEpay, model.PaymentProviderStripe, model.PaymentProviderCreem, model.PaymentProviderWaffo, model.PaymentProviderWaffoPancake}
		settle := func(provider, trade string) error {
			switch provider {
			case model.PaymentProviderAlipayF2F:
				return model.RechargeAlipayF2F(trade, decimal.NewFromInt(1), "")
			case model.PaymentProviderEpay:
				_, err := model.RechargeEpay(trade, "alipay", "")
				return err
			case model.PaymentProviderStripe:
				return model.Recharge(trade, "test-customer", "")
			case model.PaymentProviderCreem:
				return model.RechargeCreem(trade, "", "", "")
			case model.PaymentProviderWaffo:
				return model.RechargeWaffo(trade, "")
			default:
				return model.RechargeWaffoPancake(trade)
			}
		}
		for i, provider := range providers {
			order := model.TopUp{UserId: invited.Id, Amount: 1, Money: 1, QuotaSnapshot: 500000, TradeNo: "referral-" + provider, PaymentMethod: provider, PaymentProvider: provider, Status: common.TopUpStatusPending, CreateTime: common.GetTimestamp()}
			if provider == model.PaymentProviderCreem {
				order.Amount = 500000
			}
			require.NoError(t, order.Insert())
			require.NoError(t, settle(provider, order.TradeNo))
			_ = settle(provider, order.TradeNo) // Some gateways return an already-complete error.
			require.NoError(t, db.First(&inviter, inviter.Id).Error)
			assert.Equal(t, 700000+(i+1)*62500, inviter.AffQuota)
			assert.Equal(t, inviter.AffQuota, inviter.AffHistoryQuota)
			require.NoError(t, db.First(&order, order.Id).Error)
			assert.Equal(t, inviter.Id, order.ReferralUserID)
			assert.EqualValues(t, 62500, order.ReferralQuota)
		}
		t.Run("admin history exposes recorded rebates while self history stays private", func(t *testing.T) {
			require.NoError(t, db.AutoMigrate(&model.UserSession{}))
			admin := model.User{Username: "referral_history_admin", Role: common.RoleAdminUser, Status: common.UserStatusEnabled, AuthVersion: 1, AffCode: "history-admin"}
			require.NoError(t, db.Create(&admin).Error)
			adminSession, err := service.CreateLoginSession(admin.Id, "password", "127.0.0.1", "history-test")
			require.NoError(t, err)
			userSession, err := service.CreateLoginSession(invited.Id, "password", "127.0.0.1", "history-test")
			require.NoError(t, err)
			router := gin.New()
			router.GET("/api/user/topup", middleware.AdminAuth(), GetAllTopUps)
			router.GET("/api/user/topup/self", middleware.UserAuth(), GetUserTopUps)
			for _, path := range []string{"/api/user/topup?page_size=100", "/api/user/topup?keyword=referral-alipay_f2f"} {
				request := httptest.NewRequest(http.MethodGet, path, nil)
				request.Header.Set("Authorization", "Bearer "+adminSession.AccessToken)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				require.Equal(t, http.StatusOK, response.Code)
				var body struct {
					Success bool `json:"success"`
					Data    struct {
						Items []map[string]any `json:"items"`
					} `json:"data"`
				}
				require.NoError(t, common.Unmarshal(response.Body.Bytes(), &body))
				require.True(t, body.Success)
				found := false
				for _, item := range body.Data.Items {
					if item["trade_no"] != "referral-alipay_f2f" {
						continue
					}
					found = true
					assert.EqualValues(t, invited.Id, item["user_id"])
					assert.EqualValues(t, 1, item["money"])
					assert.EqualValues(t, inviter.Id, item["referral_user_id"])
					assert.EqualValues(t, 62500, item["referral_quota"])
					assert.NotContains(t, item, "quota_snapshot")
				}
				assert.True(t, found, "recorded order must be present in %s", path)
			}
			for _, path := range []string{"/api/user/topup/self", "/api/user/topup/self?keyword=referral-alipay_f2f"} {
				request := httptest.NewRequest(http.MethodGet, path, nil)
				request.Header.Set("Authorization", "Bearer "+userSession.AccessToken)
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				require.Equal(t, http.StatusOK, response.Code)
				assert.Contains(t, response.Body.String(), "referral-alipay_f2f")
				assert.NotContains(t, response.Body.String(), "referral_user_id")
				assert.NotContains(t, response.Body.String(), "referral_quota")
			}
			for _, token := range []string{"", userSession.AccessToken} {
				request := httptest.NewRequest(http.MethodGet, "/api/user/topup", nil)
				if token != "" {
					request.Header.Set("Authorization", "Bearer "+token)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				assert.Contains(t, []int{http.StatusUnauthorized, http.StatusForbidden}, response.Code)
				assert.NotContains(t, response.Body.String(), "referral_quota")
			}
		})
		// A repeated manual completion cannot add a second rebate either.
		require.NoError(t, model.ManualCompleteTopUp("referral-alipay_f2f", ""))
		starting := inviter.AffQuota
		for _, tc := range []struct {
			name         string
			age          int64
			percent      string
			self, manual bool
		}{
			{"expired", 31 * 86400, "12.5", false, false},
			{"boundary", 30 * 86400, "12.5", false, false},
			{"unknown-registration", 0, "12.5", false, false},
			{"disabled", 86400, "0", false, false},
			{"self-invite", 86400, "12.5", true, false},
			{"manual", 86400, "12.5", false, true},
		} {
			created := common.GetTimestamp() - tc.age
			if tc.age == 0 {
				created = 0
			}
			parent := inviter.Id
			if tc.self {
				parent = invited.Id
			}
			require.NoError(t, db.Model(&invited).Updates(map[string]any{"inviter_id": parent, "created_at": created}).Error)
			require.NoError(t, model.UpdateOption("referral_setting.percent", tc.percent))
			o := model.TopUp{UserId: invited.Id, Amount: 1, Money: 1, QuotaSnapshot: 500000, TradeNo: "referral-" + tc.name, PaymentMethod: model.PaymentProviderAlipayF2F, PaymentProvider: model.PaymentProviderAlipayF2F, Status: common.TopUpStatusPending}
			require.NoError(t, o.Insert())
			if tc.manual {
				require.NoError(t, model.ManualCompleteTopUp(o.TradeNo, ""))
			} else {
				require.NoError(t, settle(model.PaymentProviderAlipayF2F, o.TradeNo))
			}
			require.NoError(t, db.First(&o, o.Id).Error)
			assert.Zero(t, o.ReferralQuota, tc.name)
		}
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Equal(t, starting, inviter.AffQuota)
		// Two callbacks for the same order still award only once on every SQL engine.
		require.NoError(t, db.Model(&invited).Updates(map[string]any{"inviter_id": inviter.Id, "created_at": common.GetTimestamp() - 86400}).Error)
		concurrent := model.TopUp{UserId: invited.Id, Money: 1, QuotaSnapshot: 500000, TradeNo: "referral-concurrent", PaymentMethod: model.PaymentProviderAlipayF2F, PaymentProvider: model.PaymentProviderAlipayF2F, Status: common.TopUpStatusPending}
		require.NoError(t, concurrent.Insert())
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for range 2 {
			wg.Go(func() { errs <- settle(model.PaymentProviderAlipayF2F, concurrent.TradeNo) })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Equal(t, starting+62500, inviter.AffQuota)
		newReferralOrder := func(name string, quota int64) model.TopUp {
			o := model.TopUp{UserId: invited.Id, Money: 1, QuotaSnapshot: quota, TradeNo: "referral-" + name, PaymentMethod: model.PaymentProviderAlipayF2F, PaymentProvider: model.PaymentProviderAlipayF2F, Status: common.TopUpStatusPending}
			require.NoError(t, o.Insert())
			return o
		}
		// Neither a mismatched amount nor a failed rebate write can partially settle.
		atomicOrder := newReferralOrder("atomic", 500000)
		require.NoError(t, db.First(&invited, invited.Id).Error)
		payerBefore, earningsBefore := invited.Quota, inviter.AffQuota
		require.ErrorIs(t, model.RechargeAlipayF2F(atomicOrder.TradeNo, decimal.NewFromInt(2), ""), model.ErrAlipayAmountMismatch)
		require.NoError(t, db.Model(&inviter).Update("aff_history", common.MaxWalletQuota).Error)
		require.Error(t, settle(model.PaymentProviderAlipayF2F, atomicOrder.TradeNo))
		require.NoError(t, db.First(&atomicOrder, atomicOrder.Id).Error)
		require.NoError(t, db.First(&invited, invited.Id).Error)
		assert.Equal(t, common.TopUpStatusPending, atomicOrder.Status)
		assert.Zero(t, atomicOrder.ReferralQuota)
		assert.Equal(t, payerBefore, invited.Quota)
		require.NoError(t, db.Model(&inviter).Update("aff_history", earningsBefore).Error)
		// Failure to save the per-order record also rolls back both balances.
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("referral_ledger_failure", func(tx *gorm.DB) {
			if values, ok := tx.Statement.Dest.(map[string]any); ok && tx.Statement.Table == "top_ups" {
				if _, exists := values["referral_quota"]; exists {
					tx.AddError(errors.New("injected referral ledger failure"))
				}
			}
		}))
		settleErr := settle(model.PaymentProviderAlipayF2F, atomicOrder.TradeNo)
		require.NoError(t, db.Callback().Update().Remove("referral_ledger_failure"))
		require.Error(t, settleErr)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		require.NoError(t, db.First(&invited, invited.Id).Error)
		assert.Equal(t, earningsBefore, inviter.AffQuota)
		assert.Equal(t, payerBefore, invited.Quota)
		require.NoError(t, settle(model.PaymentProviderAlipayF2F, atomicOrder.TradeNo))
		// Configured periods and rates apply at confirmation, with downward rounding.
		for i, tc := range []struct {
			percent, days string
			quota, want   int64
		}{
			{"12.5", "30", 7, 0}, {"12.5", "30", 15, 1},
			{"100", "30", 15, 15}, {"100", "1", 15, 0},
		} {
			require.NoError(t, model.UpdateOption("referral_setting.percent", tc.percent))
			require.NoError(t, model.UpdateOption("referral_setting.days", tc.days))
			o := newReferralOrder(fmt.Sprintf("rounding-%d", i), tc.quota)
			require.NoError(t, settle(model.PaymentProviderAlipayF2F, o.TradeNo))
			require.NoError(t, db.First(&o, o.Id).Error)
			assert.Equal(t, tc.want, o.ReferralQuota)
		}
		require.NoError(t, model.UpdateOption("referral_setting.percent", "12.5"))
		require.NoError(t, model.UpdateOption("referral_setting.days", "30"))
		for _, disabled := range []bool{true, false} {
			parent := inviter.Id
			if disabled {
				require.NoError(t, db.Model(&inviter).Update("status", common.UserStatusDisabled).Error)
			} else {
				parent = -1
			}
			require.NoError(t, db.Model(&invited).Update("inviter_id", parent).Error)
			o := newReferralOrder(fmt.Sprintf("ineligible-inviter-%t", disabled), 500000)
			require.NoError(t, settle(model.PaymentProviderAlipayF2F, o.TradeNo))
			require.NoError(t, db.First(&o, o.Id).Error)
			assert.Zero(t, o.ReferralQuota)
		}
		require.NoError(t, db.Model(&inviter).Update("status", common.UserStatusEnabled).Error)
		require.NoError(t, db.Model(&invited).Update("inviter_id", inviter.Id).Error)
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		earningsBefore = inviter.AffQuota
		orders := []model.TopUp{newReferralOrder("parallel-a", 500000), newReferralOrder("parallel-b", 500000)}
		errs = make(chan error, len(orders))
		for _, o := range orders {
			wg.Go(func() { errs <- settle(model.PaymentProviderAlipayF2F, o.TradeNo) })
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			require.NoError(t, err)
		}
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Equal(t, earningsBefore+125000, inviter.AffQuota, "concurrent orders must not lose either rebate")
		// A transfer preserves lifetime earnings and changes only the two balances.
		lifetime := inviter.AffHistoryQuota
		beforeTransfer := inviter.AffQuota
		require.NoError(t, inviter.TransferAffQuotaToQuota(500000))
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Equal(t, 500000, inviter.Quota)
		assert.Equal(t, beforeTransfer-500000, inviter.AffQuota)
		assert.Equal(t, lifetime, inviter.AffHistoryQuota)
		require.Error(t, inviter.TransferAffQuotaToQuota(-500000))
		require.Error(t, inviter.TransferAffQuotaToQuota(common.MaxWalletQuota))
		require.NoError(t, db.Model(&inviter).Update("quota", common.MaxWalletQuota).Error)
		beforeTransfer = inviter.AffQuota
		require.Error(t, inviter.TransferAffQuotaToQuota(500000))
		require.NoError(t, db.First(&inviter, inviter.Id).Error)
		assert.Equal(t, beforeTransfer, inviter.AffQuota, "overflow must not consume earnings")
	})

	t.Run("site theme persists across cache reload and rejects failed writes", func(t *testing.T) {
		common.OptionMapRWMutex.Lock()
		previousOptions := common.OptionMap
		common.OptionMap = map[string]string{}
		common.OptionMapRWMutex.Unlock()
		t.Cleanup(func() {
			common.OptionMapRWMutex.Lock()
			common.OptionMap = previousOptions
			common.OptionMapRWMutex.Unlock()
		})
		key := system_setting.ThemeDefaultPresetOptionKey
		old := system_setting.GetThemeSetting().DefaultPreset
		t.Cleanup(func() { require.NoError(t, model.UpdateOption(key, old)) })
		require.NoError(t, model.UpdateOption(key, "new-utopia"))
		assert.Equal(t, "new-utopia", system_setting.GetThemeSetting().DefaultPreset)
		require.NoError(t, db.Callback().Update().Before("gorm:update").Register("theme_setting_failure", func(tx *gorm.DB) {
			if tx.Statement.Table == "options" {
				tx.AddError(errors.New("injected theme settings failure"))
			}
		}))
		updateErr := model.UpdateOption(key, "default")
		require.NoError(t, db.Callback().Update().Remove("theme_setting_failure"))
		require.Error(t, updateErr)
		assert.Equal(t, "new-utopia", system_setting.GetThemeSetting().DefaultPreset)
		system_setting.GetThemeSetting().DefaultPreset = "default"
		require.NoError(t, model.LoadOptionsFromDatabase())
		assert.Equal(t, "new-utopia", system_setting.GetThemeSetting().DefaultPreset)
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
