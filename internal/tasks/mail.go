package tasks

import (
	"encoding/json"
	"io"
	"io/ioutil"

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

func taskSendMail(paramValues string) error {
	var conn emailConn
	if err := json.Unmarshal([]byte(paramValues), &conn); err != nil {
		return err
	}
	mail := gomail.NewMessage()
	mail.SetHeader("From", conn.SenderAddr)

	//Multiple Recipeints addresses
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
		content, err := ioutil.ReadFile(attachment)
		if err != nil {
			return err
		}
		mail.Attach(attachment, gomail.SetCopyFunc(func(w io.Writer) error {
			_, err = w.Write(content)
			return err
		}))
	}

	dialer := gomail.NewDialer(conn.ServerHost, conn.ServerPort, conn.Username, conn.Password)
	s, err := dialer.Dial()
	if err != nil {
		return err
	}
	return gomail.Send(s, mail)
}
