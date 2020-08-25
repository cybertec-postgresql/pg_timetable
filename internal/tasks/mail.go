package tasks

import (
	"context"
	"encoding/json"
	"errors"

	"gopkg.in/gomail.v2"
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
	DialAndSend(m ...*gomail.Message) error
}

var getNewDialer func(host string, port int, username, password string) Dialer = func(host string, port int, username, password string) Dialer {
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

	return sendMail(conn)
}

func sendMail(conn emailConn) error {
	mail := gomail.NewMessage()
	mail.SetHeader("From", conn.SenderAddr)

	//Multiple Recipients addresses
	torecipients := make([]string, len(conn.ToAddr))
	for i, toAddr := range conn.ToAddr {
		torecipients[i] = mail.FormatAddress(toAddr, " ")
	}
	mail.SetHeader("To", torecipients...)

	// Multiple CC Addresses
	ccrecipients := make([]string, len(conn.CcAddr))
	for i, ccaddr := range conn.CcAddr {
		ccrecipients[i] = mail.FormatAddress(ccaddr, " ")
	}
	mail.SetHeader("Cc", ccrecipients...)

	// Multiple bCC Addresses
	bccrecipients := make([]string, len(conn.BccAddr))
	for i, bccaddr := range conn.BccAddr {
		bccrecipients[i] = mail.FormatAddress(bccaddr, " ")
	}
	mail.SetHeader("Bcc", bccrecipients...)

	mail.SetHeader("Subject", conn.Subject)
	mail.SetBody("text/html", conn.MsgBody)

	//attach multiple documents
	for _, attachment := range conn.Attachments {
		mail.Attach(attachment)
	}
	// Send Mail
	dialer := getNewDialer(conn.ServerHost, conn.ServerPort, conn.Username, conn.Password)
	return dialer.DialAndSend(mail)
}
