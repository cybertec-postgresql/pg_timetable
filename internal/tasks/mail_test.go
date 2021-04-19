package tasks

import (
	"context"
	"testing"

	gomail "github.com/ory/mail/v3"
	"github.com/stretchr/testify/assert"
)

type fakeDialer struct {
	Dialer
}

func (d *fakeDialer) DialAndSend(ctx context.Context, m ...*gomail.Message) error {
	return nil
}

func TestTaskSendMail(t *testing.T) {
	assert.NotNil(t, NewDialer("", 0, "", ""), "Default dialer should be created")
	NewDialer = func(host string, port int, username, password string) Dialer {
		return &fakeDialer{}
	}
	assert.NoError(t, SendMail(context.Background(), EmailConn{
		ServerHost:  "smtp.example.com",
		ServerPort:  587,
		Username:    "user",
		Password:    "pwd",
		SenderAddr:  "abc@example.com",
		ToAddr:      []string{"to@example.com"},
		CcAddr:      []string{"cc@example.com"},
		BccAddr:     []string{"bcc@example.com"},
		Attachments: []string{"mail.go"},
	}), "Sending email with required json input should succeed")
}
