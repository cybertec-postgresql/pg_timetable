package tasks

import (
	"encoding/json"
	"net/smtp"
)

type emailConn struct {
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	ServerHost string   `json:"serverhost"`
	ServerPort string   `json:"serverport"`
	SenderAddr string   `json:"senderaddr"`
	ToAddr     []string `json:"toaddr"`
	MsgBody    string   `json:"msgbody"`
}

func taskSendMail(paramValues string) error {
	var conn emailConn
	if err := json.Unmarshal([]byte(paramValues), &conn); err != nil {
		return err
	}
	addr := conn.ServerHost + ":" + conn.ServerPort
	auth := smtp.PlainAuth("", conn.Username, conn.Password, conn.ServerHost)
	return smtp.SendMail(addr, auth, conn.SenderAddr, conn.ToAddr, []byte(conn.MsgBody))
}
