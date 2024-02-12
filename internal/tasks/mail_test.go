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

func (d *fakeDialer) DialAndSend(context.Context, ...*gomail.Message) error {
	return nil
}

func TestTaskSendMail(t *testing.T) {
	assert.NotNil(t, NewDialer("", 0, "", ""), "Default dialer should be created")
	NewDialer = func(string, int, string, string) Dialer {
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
		AttachmentData: []EmailAttachmentData{{
			Name:       "File1.txt",
			Base64Data: []byte("RmlsZSBDb250ZW50"), // "File Content" base64-encoded
		}},
	}), "Sending email with required json input should succeed")
}
