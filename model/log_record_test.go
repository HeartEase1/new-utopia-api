package model

import (
	"net/http/httptest"
	"os"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/relaykit/dto"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

func TestRequestLogsAlwaysStoreClientIP(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			var driver gorm.Dialector
			switch dialect {
			case "sqlite":
				driver = sqlite.Open(":memory:")
			case "mysql":
				dsn := os.Getenv("TEST_MYSQL_DSN")
				if dsn == "" {
					t.Skip("TEST_MYSQL_DSN is not configured")
				}
				driver = mysql.Open(dsn)
			case "postgres":
				dsn := os.Getenv("TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("TEST_POSTGRES_DSN is not configured")
				}
				driver = postgres.Open(dsn)
			}
			logDB, err := gorm.Open(driver, &gorm.Config{
				NamingStrategy: schema.NamingStrategy{TablePrefix: "request_log_ip_test_"},
			})
			require.NoError(t, err)
			sqlDB, err := logDB.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() {
				assert.NoError(t, logDB.Migrator().DropTable(&Log{}, &User{}))
				assert.NoError(t, sqlDB.Close())
			})
			require.NoError(t, logDB.Migrator().DropTable(&Log{}, &User{}))
			require.NoError(t, logDB.AutoMigrate(&Log{}, &User{}))

			previousDB := DB
			previousLogDB := LOG_DB
			previousRedisEnabled := common.RedisEnabled
			previousLogConsumeEnabled := common.LogConsumeEnabled
			previousMainDatabaseType := common.MainDatabaseType()
			previousLogDatabaseType := common.LogDatabaseType()
			DB = logDB
			LOG_DB = logDB
			common.RedisEnabled = false
			common.LogConsumeEnabled = true
			common.SetDatabaseTypes(common.DatabaseType(dialect), common.DatabaseType(dialect))
			t.Cleanup(func() {
				DB = previousDB
				LOG_DB = previousLogDB
				common.RedisEnabled = previousRedisEnabled
				common.LogConsumeEnabled = previousLogConsumeEnabled
				common.SetDatabaseTypes(previousMainDatabaseType, previousLogDatabaseType)
			})

			versionSQL := "SELECT version()"
			if dialect == "sqlite" {
				versionSQL = "SELECT sqlite_version()"
			}
			var version string
			require.NoError(t, logDB.Raw(versionSQL).Scan(&version).Error)
			t.Logf("database version: %s", version)

			user := User{Username: "ip-recording-test", Password: "test-only"}
			require.NoError(t, logDB.Create(&user).Error)
			require.NoError(t, UpdateUserSetting(user.Id, dto.UserSetting{RecordIpLog: false}))
			require.NoError(t, logDB.First(&user, user.Id).Error)
			assert.True(t, user.GetSetting().RecordIpLog)
			var persistedSetting dto.UserSetting
			require.NoError(t, common.Unmarshal([]byte(user.Setting), &persistedSetting))
			assert.True(t, persistedSetting.RecordIpLog)

			clientIP := "203.0.113.17"
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
			ctx.Request.RemoteAddr = clientIP + ":4321"
			ctx.Set("username", "user")

			RecordErrorLog(ctx, user.Id, 2, "model", "token", "error", 3, 1, true, "default", NewLogOther())
			RecordConsumeLog(ctx, user.Id, RecordConsumeLogParams{
				ModelName: "model",
				TokenName: "token",
				Other:     NewLogOther(),
			})

			var logs []Log
			require.NoError(t, logDB.Find(&logs).Error)
			require.Len(t, logs, 2)
			assert.ElementsMatch(t, []int{LogTypeConsume, LogTypeError}, []int{logs[0].Type, logs[1].Type})
			for _, log := range logs {
				assert.Equal(t, clientIP, log.Ip)
			}
		})
	}
}
