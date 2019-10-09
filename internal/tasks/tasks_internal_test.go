package tasks

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDownloadFile(t *testing.T) {
	downloadUrls = func(urls []string, dest string, workers int) error { return nil }
	assert.EqualError(t, taskDownloadFile(""), `unexpected end of JSON input`,
		"Download with empty param should fail")
	assert.EqualError(t, taskDownloadFile(`{"workersnum": 0, "fileurls": [] }`),
		"Files to download are not specified", "Download with empty files should fail")
	assert.Error(t, taskDownloadFile(`{"workersnum": 0, "fileurls": ["http://foo.bar"], "destpath": "non-existent" }`),
		"Downlod with non-existent directory or insufficient rights should fail")
	assert.NoError(t, taskDownloadFile(`{"workersnum": 0, "fileurls": ["http://foo.bar"], "destpath": "." }`),
		"Downlod with correct json input should succeed")
}

func TestTaskSendMail(t *testing.T) {
	sendMail = func(m emailConn) error { return nil }
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
		"SenderAddr":"abc@example.com","ToAddr":["to@example.com"],"CcAddr":["cc@example.com"],"BccAddr":["bcc@example.com"]}`),
		"Sending email with required json input should succeed")
}
