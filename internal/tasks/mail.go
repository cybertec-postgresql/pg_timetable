package tasks

import (
	"context"
	"encoding/json"
	"errors"

	gomail "github.com/ory/mail/v3"
)

type emailConn struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	ServerHost  string   `json:"serverhost"`
	ServerPort  int      `json:"serverport"`
	SenderAddr  string   `json:"senderaddr"`
	CcAddr      []string `json:"ccaddr"`
	BccAddr     []string `json:"bccaddr"`
	ToAddr      []string `json:"toaddr"`
	Subject     string   `json:"subject"`
	MsgBody     string   `json:"msgbody"`
	Attachments []string `json:"attachment"`
}

// Dialer implements DialAndSend function for mailer
type Dialer interface {
	DialAndSend(ctx context.Context, m ...*gomail.Message) error
}

var NewDialer func(host string, port int, username, password string) Dialer = func(host string, port int, username, password string) Dialer {
	return gomail.NewDialer(host, port, username, password)
}

func taskSendMail(ctx context.Context, paramValues string) error {
	var conn emailConn
	if err := json.Unmarshal([]byte(paramValues), &conn); err != nil {
		return err
	}
	if conn.ServerHost == "" {
		return errors.New("The IP address or hostname of the mail server not specified")
	}
	if conn.ServerPort == 0 {
		return errors.New("The port of the mail server not specified")
	}
	if conn.Username == "" {
		return errors.New("The username used for authenticating on the mail server not specified")
	}
	if conn.Password == "" {
		return errors.New("The password used for authenticating on the mail server not specified")
	}
	if conn.SenderAddr == "" {
		return errors.New("Sender address not specified")
	}
	if len(conn.ToAddr) == 0 && len(conn.CcAddr) == 0 && len(conn.BccAddr) == 0 {
		return errors.New("Recipient address not specified")
	}

	mail := gomail.NewMessage()
	mail.SetHeader("From", conn.SenderAddr)
	mail.SetHeader("To", conn.ToAddr...)
	mail.SetHeader("Cc", conn.CcAddr...)
	mail.SetHeader("Bcc", conn.BccAddr...)
	mail.SetHeader("Subject", conn.Subject)
	mail.SetBody("text/html", conn.MsgBody)
	for _, attachment := range conn.Attachments {
		mail.Attach(attachment)
	}
	return NewDialer(conn.ServerHost, conn.ServerPort, conn.Username, conn.Password).DialAndSend(ctx, mail)
}
