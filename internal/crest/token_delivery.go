package crest

import "fmt"

const (
	tokenSubject = "[AKeyRA Bootstrap] Tuwunel Registration Token"
	tokenBody    = `A registration token for Tuwunel has been generated.

Token: %s

Instructions:
1. Open Element (or any Matrix client) on your phone.
2. Enter homeserver: https://matrix.akeyra.klazomenai.dev
3. Use this token to create your account.

This token is single-use. Rotate it in Vault after first login:
  vault kv put secret/matrix/tuwunel registration-token=<new-uuid>

Signal received. Contents verified.
— Crest`
)

// SendRegistrationToken emails the Tuwunel registration token to the Captain.
func SendRegistrationToken(cfg SMTPConfig, captainEmail, token string) error {
	body := fmt.Sprintf(tokenBody, token)
	if err := SendMail(cfg, captainEmail, tokenSubject, body); err != nil {
		return fmt.Errorf("token delivery: %w", err)
	}
	return nil
}
