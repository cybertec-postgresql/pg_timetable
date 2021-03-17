package tasks

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/gomail.v2"
)

type fakeDialer struct {
	Dialer
}

func (d *fakeDialer) DialAndSend(m ...*gomail.Message) error {
	return nil
}

func TestTaskSendMail(t *testing.T) {
	assert.NotNil(t, getNewDialer("", 0, "", ""), "Default dialer should be created")
	getNewDialer = func(host string, port int, username, password string) Dialer {
		return &fakeDialer{}
	}
	assert.NoError(t, SendMail(EmailConn{
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
