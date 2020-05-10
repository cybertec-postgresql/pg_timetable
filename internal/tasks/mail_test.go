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
	assert := assert.New(t)
	assert.Error(taskSendMail(""), `unexpected end of JSON input`,
		"Sending mail with empty param should fail")
	assert.EqualError(taskSendMail(`{"ServerHost":""}`),
		"The IP address or hostname of the mail server not specified", "Sending mail without host/IP should fail")
	assert.EqualError(taskSendMail(`{"ServerHost":"smtp.example.com","ServerPort":0}`),
		"The port of the mail server not specified", "Sending mail without port should fail")
	assert.EqualError(taskSendMail(`{"ServerHost":"smtp.example.com","ServerPort":587,"Username":""}`),
		"The username used for authenticating on the mail server not specified", "Sending mail without valid user id should fail")
	assert.EqualError(taskSendMail(`{"ServerHost":"smtp.example.com","ServerPort":587,"Username":"user","Password":""}`),
		"The password used for authenticating on the mail server not specified", "Sending mail with invalid authentication should fail")
	assert.EqualError(taskSendMail(`{"ServerHost":"smtp.example.com","ServerPort":587,"Username":"user","Password":"pwd","SenderAddr":""}`),
		"Sender address not specified", "Sending mail without a valid sender address should fail")
	assert.EqualError(taskSendMail(`{"ServerHost":"smtp.example.com","ServerPort":587,"Username":"user","Password":"pwd",
		"SenderAddr":"abc@example.com","ToAddr":[],"CcAddr":[],"BccAddr":[]}`),
		"Recipient address not specified", "Sending mail without recipient should fail")
	assert.NoError(taskSendMail(`{"ServerHost":"smtp.example.com","ServerPort":587,"Username":"user","Password":"pwd",
		"SenderAddr":"abc@example.com","ToAddr":["to@example.com"],"CcAddr":["cc@example.com"],"BccAddr":["bcc@example.com"],
		"Attachment": ["mail.go"]}`),
		"Sending email with required json input should succeed")
}
