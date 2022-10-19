package tasks

import (
	"bytes"
	"context"

	gomail "github.com/ory/mail/v3"
)

// Attachment file type
// Has pair: name and content (base64-encoded) of attachment file
type EmailAttachmentData struct {
	Name       string `json:"name"`
	Base64Data []byte `json:"base64data"`
}

// EmailConn structure represents a connection to a mail server and mail fields
type EmailConn struct {
	Username       string                `json:"username"`
	Password       string                `json:"password"`
	ServerHost     string                `json:"serverhost"`
	ServerPort     int                   `json:"serverport"`
	SenderAddr     string                `json:"senderaddr"`
	CcAddr         []string              `json:"ccaddr"`
	BccAddr        []string              `json:"bccaddr"`
	ToAddr         []string              `json:"toaddr"`
	Subject        string                `json:"subject"`
	MsgBody        string                `json:"msgbody"`
	Attachments    []string              `json:"attachment"`
	AttachmentData []EmailAttachmentData `json:"attachmentdata"`
	ContentType    string                `json:"contenttype"`
}

// Dialer implements DialAndSend function for mailer
type Dialer interface {
	DialAndSend(ctx context.Context, m ...*gomail.Message) error
}

// NewDialer returns a new gomail dialer instance
var NewDialer = func(host string, port int, username, password string) Dialer {
	return gomail.NewDialer(host, port, username, password)
}

// SendMail sends mail message specified by conn within context ctx
func SendMail(ctx context.Context, conn EmailConn) error {
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
	mail.SetBody(conn.ContentType, conn.MsgBody)

	//attach multiple documents
	for _, attachment := range conn.Attachments {
		mail.Attach(attachment)
	}

	// Attach file with contents
	for _, attachmentData := range conn.AttachmentData {
		mail.AttachReader(attachmentData.Name, bytes.NewReader([]byte(attachmentData.Base64Data)))
	}
	// Send Mail
	dialer := NewDialer(conn.ServerHost, conn.ServerPort, conn.Username, conn.Password)
	return dialer.DialAndSend(ctx, mail)
}
