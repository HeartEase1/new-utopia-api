/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.
*/
package controller

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type alipayF2FSettingsRequest struct {
	service.AlipayF2FConfig
	Revision string `json:"revision"`
}

// Responses are an allowlist: neither saved key is ever sent to a browser.
func alipayF2FSettingsView(config service.AlipayF2FConfig, raw string) gin.H {
	return gin.H{
		"enabled": config.Enabled, "sandbox": config.Sandbox, "app_id": config.AppID,
		"seller_id": config.SellerID, "notify_url": config.NotifyURL,
		"private_key_configured":       strings.TrimSpace(config.PrivateKey) != "",
		"alipay_public_key_configured": strings.TrimSpace(config.AlipayPublicKey) != "",
		"revision":                     fmt.Sprintf("%x", sha256.Sum256([]byte(raw))),
		"from_environment":             raw == "" && config.PrivateKey != "",
	}
}

func GetAlipayF2FSettings(c *gin.Context) {
	config, raw, err := service.LoadAlipayF2FConfig()
	if err != nil && raw != "" {
		common.ApiErrorT(c, "Unable to load Alipay settings.")
		return
	}
	if config.NotifyURL == "" {
		common.OptionMapRWMutex.RLock()
		base := common.OptionMap["ServerAddress"]
		common.OptionMapRWMutex.RUnlock()
		if base == "" {
			base = system_setting.ServerAddress
		}
		config.NotifyURL = strings.TrimRight(base, "/") + "/api/alipay-f2f/notify"
	}
	common.ApiSuccess(c, alipayF2FSettingsView(config, raw))
}

// Both save and connection check use the same full validation and secret merge.
// Blank key inputs retain the currently saved key, including the legacy files.
func readAlipayF2FSettingsRequest(c *gin.Context) (service.AlipayF2FConfig, string, bool) {
	if c.ContentType() != "application/json" {
		common.ApiErrorT(c, "Invalid parameters")
		return service.AlipayF2FConfig{}, "", false
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32*1024)
	var request alipayF2FSettingsRequest
	if err := common.DecodeJson(c.Request.Body, &request); err != nil {
		common.ApiErrorT(c, "Invalid parameters")
		return service.AlipayF2FConfig{}, "", false
	}
	current, raw, err := service.LoadAlipayF2FConfig()
	if err != nil && raw != "" {
		common.ApiErrorT(c, "Unable to load Alipay settings.")
		return current, raw, false
	}
	if request.Revision != fmt.Sprintf("%x", sha256.Sum256([]byte(raw))) {
		common.ApiErrorT(c, "Alipay settings changed. Reload before saving.")
		return current, raw, false
	}
	next := request.AlipayF2FConfig
	next.AppID, next.SellerID = strings.TrimSpace(next.AppID), strings.TrimSpace(next.SellerID)
	next.NotifyURL = strings.TrimSpace(next.NotifyURL)
	next.PrivateKey, next.AlipayPublicKey = strings.TrimSpace(next.PrivateKey), strings.TrimSpace(next.AlipayPublicKey)
	if next.PrivateKey == "" {
		next.PrivateKey = current.PrivateKey
	}
	if next.AlipayPublicKey == "" {
		next.AlipayPublicKey = current.AlipayPublicKey
	}
	if _, err := service.ValidateAlipayF2FConfig(next); err != nil {
		common.ApiErrorT(c, "Check the Alipay app ID, seller PID, RSA2 keys and HTTPS callback URL.")
		return current, raw, false
	}
	return next, raw, true
}

func SaveAlipayF2FSettings(c *gin.Context) {
	next, raw, ok := readAlipayF2FSettingsRequest(c)
	if !ok {
		return
	}
	if next.Enabled && !isPaymentComplianceConfirmed() {
		common.ApiErrorT(c, "Payment, redemption, subscription, and invitation reward features are disabled. The administrator must confirm compliance terms before enabling them.")
		return
	}
	current, _, _ := service.LoadAlipayF2FConfig()
	changed := current.AppID != next.AppID || current.SellerID != next.SellerID || current.Sandbox != next.Sandbox
	if changed && current.Enabled {
		common.ApiErrorT(c, "Disable Alipay and settle pending orders before changing the merchant or environment.")
		return
	}
	encoded, err := common.Marshal(next)
	if err != nil {
		common.ApiErrorT(c, "Failed to save Alipay settings.")
		return
	}
	if err := model.SaveAlipayF2FOption(raw, string(encoded), changed); err != nil {
		// Only safe, deliberately constructed business messages may leave this API.
		if _, ok := err.(*common.Message); ok {
			common.ApiError(c, err)
		} else {
			common.ApiErrorT(c, "Failed to save Alipay settings.")
		}
		return
	}
	common.ApiSuccess(c, alipayF2FSettingsView(next, string(encoded)))
}

func CheckAlipayF2FSettings(c *gin.Context) {
	config, _, ok := readAlipayF2FSettingsRequest(c)
	if !ok {
		return
	}
	client, _ := service.ValidateAlipayF2FConfig(config)
	// A query of a random, nonexistent order does not create or charge an order.
	// A signed TRADE_NOT_EXIST reply confirms the app credentials and public key.
	_, _, err := client.Query(c.Request.Context(), "UT_CHECK_"+strings.ReplaceAll(uuid.NewString(), "-", ""))
	if err != nil {
		common.ApiErrorT(c, "Alipay connection failed. Check the credentials, environment and network.")
		return
	}
	common.ApiSuccessT(c, "Alipay connection verified. Seller PID and payment callbacks still require a test payment.", nil)
}
