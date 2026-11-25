package service

import (
	"errors"
	"fmt"
	"html"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
)

type WalletTransferPasswordFlow struct {
	FlowToken string `json:"flow_token"`
	Email     string `json:"email"`
	// EmailExempt is true for the root user without a bound email: no code was sent.
	EmailExempt bool  `json:"email_exempt"`
	ExpiresAt   int64 `json:"expires_at"`
}

var ErrWalletTransferPasswordEmailDelivery = errors.New("Verification email could not be sent. Start again.")

func StartWalletTransferPasswordFlow(identity AuthIdentity, intent, language string) (*WalletTransferPasswordFlow, error) {
	if intent != "set" && intent != "change" && intent != "reset" {
		return nil, model.ErrWalletPasswordFlowInvalid
	}
	codes, err := generateEmailBindingCodes(false)
	if err != nil {
		return nil, err
	}
	token, expiresAt, email, err := model.CreateWalletTransferPasswordFlow(identity, intent, codes.NewHash)
	if err != nil {
		return nil, err
	}
	if email == "" {
		return &WalletTransferPasswordFlow{FlowToken: token, EmailExempt: true, ExpiresAt: expiresAt.Unix()}, nil
	}
	if err := sendWalletTransferPasswordCode(language, email, codes.New); err != nil {
		_, _ = model.ConsumeAuthFlow(token, model.AuthFlowMatch{
			Purpose: model.WalletPasswordFlowPurpose, UserId: identity.UserID, SessionId: identity.SessionID,
		})
		return nil, ErrWalletTransferPasswordEmailDelivery
	}
	return &WalletTransferPasswordFlow{
		FlowToken: token,
		Email:     common.MaskEmail(email),
		ExpiresAt: expiresAt.Unix(),
	}, nil
}

func CompleteWalletTransferPasswordFlow(identity AuthIdentity, token, code, currentPassword, loginPassword, twoFACode, newPassword string) error {
	if err := common.ValidateNewAccountPassword(newPassword); err != nil {
		return err
	}
	if common.IsCommonPassword(newPassword) {
		return common.ErrPasswordTooCommon
	}
	if err := model.ValidateWalletTransferPasswordCode(identity, token, code); err != nil {
		return err
	}
	passwordHash, err := common.HashAccountPassword(newPassword)
	if err != nil {
		return err
	}
	return model.CompleteWalletTransferPasswordFlow(identity, token, code, currentPassword, loginPassword, twoFACode, newPassword, passwordHash)
}

func sendWalletTransferPasswordCode(language, email, code string) error {
	data := map[string]any{"SystemName": common.SystemName}
	subject := i18n.Translate(language, i18n.MsgEmailVerificationSubject, data)
	content := fmt.Sprintf("<p>%s</p><p>%s</p><p>%s</p>",
		i18n.Translate(language, i18n.MsgEmailVerificationGreeting, data),
		i18n.Translate(language, i18n.MsgEmailVerificationCode, map[string]any{"Code": html.EscapeString(code)}),
		i18n.Translate(language, i18n.MsgEmailVerificationValidity, map[string]any{"Minutes": int(model.WalletPasswordFlowTTL / time.Minute)}),
	)
	return common.SendEmail(subject, email, content)
}
