package tasks

import (
	"gopkg.in/gomail.v2"
)

type EmailConn struct {
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

func SendMail(conn EmailConn) error {
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
